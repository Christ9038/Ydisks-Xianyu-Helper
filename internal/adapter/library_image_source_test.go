package adapter

import (
	"context"
	"errors"
	"strings"
	"testing"

	cardsapp "xianyu-go/internal/application/cards"
	chatapp "xianyu-go/internal/application/chat"
	defaultreplyapp "xianyu-go/internal/application/defaultreply"
	deliveryapp "xianyu-go/internal/application/deliverytemplate"
	itemgreetingapp "xianyu-go/internal/application/itemgreeting"
	keywordsapp "xianyu-go/internal/application/keywords"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/deliverytemplate"
)

// ownerKey 是所有者图片符合服务端存储键格式的测试键。
var ownerKey = "1/" + strings.Repeat("a", 64) + ".png"

// libraryImageFixture 保存两个用户各自的账号与图片库图片，用于验证按账号所有者隔离。
type libraryImageFixture struct {
	// store 是迁移后的测试数据库聚合。
	store *db.Store
	// source 是读取图片字节的来源，文件存放在测试临时目录。
	source *LibraryImageSource
	// ownerID、ownerAccount、ownerImageID 是图片所有者、其账号和其图片。
	ownerID, ownerImageID int64
	ownerAccount          string
	// otherAccount、otherImageID 是另一用户的账号和图片。
	otherAccount string
	otherImageID int64
}

// newLibraryImageFixture 创建两个用户、各自账号和一张已落盘的图片库图片。
func newLibraryImageFixture(t *testing.T) (*libraryImageFixture, func()) {
	t.Helper()
	// store、cleanup 是迁移后的测试数据库及关闭函数。
	store, cleanup := newDeliveryTemplateAdapterStore(t)
	// ctx 是本夹具共用的上下文。
	ctx := context.Background()
	// files 把图片字节保存到测试临时目录。
	files, err := NewLibraryFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// seed 为一个用户创建账号和图片，并写入图片字节。
	seed := func(username, account, key string, content []byte) (int64, int64) {
		// execErr 保存插入测试用户的失败原因，非空即夹具构造失败；result 是插入用户后的执行结果，用于取得新用户主键。
		result, execErr := store.DB.ExecContext(ctx, `INSERT INTO users (username, email, password_hash) VALUES (?,?,'x')`, username, username+"@example.test")
		if execErr != nil {
			t.Fatal(execErr)
		}
		// userID 是新建测试用户的主键，作为账号与图片的所有者。
		userID, _ := result.LastInsertId()
		// saveErr 保存写入账号或图片字节的失败原因。
		if saveErr := store.Cookies.Save(ctx, account, "unb=1;", userID); saveErr != nil {
			t.Fatal(saveErr)
		}
		// saveErr 保存写入账号或图片字节的失败原因。
		if saveErr := files.Save(ctx, key, content); saveErr != nil {
			t.Fatal(saveErr)
		}
		// image 是写入图片库的图片元数据（含主键）；insertErr 保存插入图片库元数据的失败原因。
		image, _, insertErr := store.LibraryImages.Insert(ctx, db.LibraryImage{UserID: userID, Filename: username + ".png", StorageKey: key, ContentType: "image/png", SizeBytes: int64(len(content)), SHA256: username})
		if insertErr != nil {
			t.Fatal(insertErr)
		}
		return userID, image.ID
	}
	// fixture 是本用例使用的双用户图片库夹具。
	fixture := &libraryImageFixture{store: store, source: &LibraryImageSource{store: store, files: files}, ownerAccount: "acc-owner", otherAccount: "acc-other"}
	fixture.ownerID, fixture.ownerImageID = seed("owner", "acc-owner", ownerKey, []byte("owner-bytes"))
	_, fixture.otherImageID = seed("other", "acc-other", "2/"+strings.Repeat("b", 64)+".png", []byte("other-bytes"))
	return fixture, cleanup
}

// TestLibraryImageSourceReadsOnlyAccountOwnerImages 验证读取按发送账号的所有者隔离，并覆盖各类不可用分支。
func TestLibraryImageSourceReadsOnlyAccountOwnerImages(t *testing.T) {
	// cleanup 关闭测试数据库并释放临时资源；fixture 是本用例使用的双用户图片库夹具。
	fixture, cleanup := newLibraryImageFixture(t)
	defer cleanup()
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	// data、contentType、filename、err 保存所有者读取自己图片的结果。
	data, contentType, filename, err := fixture.source.ReadLibraryImage(ctx, fixture.ownerAccount, fixture.ownerImageID)
	if err != nil || string(data) != "owner-bytes" || contentType != "image/png" || filename != "owner.png" {
		t.Fatalf("data=%q type=%q name=%q err=%v", data, contentType, filename, err)
	}
	// call 是待执行的一个不可用场景，返回该场景下读取图片得到的错误；name 是当前场景的名称，用于失败信息定位。
	for name, call := range map[string]func() error{
		"他人图片": func() error {
			// e 为本处局部值。
			_, _, _, e := fixture.source.ReadLibraryImage(ctx, fixture.ownerAccount, fixture.otherImageID)
			return e
		},
		"不存在的图片": func() error {
			// e 为本处局部值。
			_, _, _, e := fixture.source.ReadLibraryImage(ctx, fixture.ownerAccount, 9999)
			return e
		},
		"不存在的账号": func() error {
			// e 为本处局部值。
			_, _, _, e := fixture.source.ReadLibraryImage(ctx, "ghost", fixture.ownerImageID)
			return e
		},
		// e 为本处局部值。
		"空账号": func() error { _, _, _, e := fixture.source.ReadLibraryImage(ctx, " ", fixture.ownerImageID); return e },
		// e 为本处局部值。
		"零主键": func() error { _, _, _, e := fixture.source.ReadLibraryImage(ctx, fixture.ownerAccount, 0); return e },
		// e 为本处局部值。
		"未装配": func() error { _, _, _, e := (*LibraryImageSource)(nil).ReadLibraryImage(ctx, "a", 1); return e },
	} {
		// callErr 为本处局部值。
		if callErr := call(); !errors.Is(callErr, ErrLibraryImageUnavailable) {
			t.Fatalf("%s 应返回 ErrLibraryImageUnavailable: %v", name, callErr)
		}
	}
	// 文件被外部删除后读取失败，且错误不暴露本地路径。
	if removeErr := fixture.source.files.Remove(ctx, ownerKey); removeErr != nil {
		t.Fatal(removeErr)
	}
	// readErr 为本处局部值。
	if _, _, _, readErr := fixture.source.ReadLibraryImage(ctx, fixture.ownerAccount, fixture.ownerImageID); !errors.Is(readErr, ErrLibraryImageUnavailable) {
		t.Fatalf("文件丢失应不可用: %v", readErr)
	}
}

// TestAutomationImageSenderSendsLibraryImageThroughSharedUploadPath 验证图片库图片复用上传与真实宽高，失败一律为确定未发送。
func TestAutomationImageSenderSendsLibraryImageThroughSharedUploadPath(t *testing.T) {
	// sender 记录最终交给账号运行时的参数，uploader 返回闲鱼上传后的地址和宽高。
	sender := &automationImageSenderStub{}
	// uploader 为本处局部值。
	uploader := &automationImageUploaderStub{result: chatapp.ImageUpload{URL: "https://cdn.example/lib.png", Width: 640, Height: 480}}
	// reader 为本处局部值。
	reader := func(_ context.Context, accountID string, imageID int64) ([]byte, string, string, error) {
		if accountID != "acc" || imageID != 5 {
			return nil, "", "", errors.New("unexpected")
		}
		return []byte("lib"), "image/png", "lib.png", nil
	}
	// wrapper 为本处局部值。
	wrapper := automationImageSender{accountID: "acc", sender: sender, uploader: uploader, libraryImages: reader}
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if err := wrapper.SendLibraryImage(context.Background(), "chat", "buyer", 5, 9); err != nil {
		t.Fatal(err)
	}
	if sender.imageURL != "https://cdn.example/lib.png" || sender.width != 640 || sender.height != 480 || sender.cardID != 9 || uploader.accountID != "acc" || string(uploader.data) != "lib" {
		t.Fatalf("sender=%+v uploader=%+v", sender, uploader)
	}
	// broken 为本处局部值；name 是当前场景的名称，用于失败信息定位。
	for name, broken := range map[string]automationImageSender{
		"读取失败": {accountID: "acc", sender: sender, uploader: uploader, libraryImages: func(context.Context, string, int64) ([]byte, string, string, error) {
			return nil, "", "", errors.New("图片已删除")
		}},
		"上传失败": {accountID: "acc", sender: sender, uploader: &automationImageUploaderStub{err: errors.New("上传失败")}, libraryImages: reader},
		"未装配":  {accountID: "acc", sender: sender, uploader: uploader},
	} {
		// calls 记录失败前已发生的 WebSocket 发送次数，失败路径不得增加。
		calls := sender.imageCalls
		// err 保存被测调用的失败原因，断言时按预期分支检查。
		if err := broken.SendLibraryImage(context.Background(), "chat", "buyer", 5, 0); !errors.Is(err, automation.ErrMessageNotSent) || sender.imageCalls != calls {
			t.Fatalf("%s 应为确定未发送且不触发 WebSocket: %v", name, err)
		}
	}
}

// TestRepositoriesRejectForeignLibraryImages 验证各配置仓储拒绝引用他人图片，并接受自己的图片。
func TestRepositoriesRejectForeignLibraryImages(t *testing.T) {
	// cleanup 关闭测试数据库并释放临时资源；fixture 是本用例使用的双用户图片库夹具。
	fixture, cleanup := newLibraryImageFixture(t)
	defer cleanup()
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	// store 为本处局部值。
	store := fixture.store

	// keywords：他人图片按校验错误处理。
	keywords := NewKeywordRepository(store)
	// keywordValidation 为本处局部值。
	var keywordValidation *keywordsapp.ValidationError
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if _, err := keywords.Add(ctx, fixture.ownerID, fixture.ownerAccount, keywordsapp.Draft{Keyword: "图", Type: "image", LibraryImageID: fixture.otherImageID}); !errors.As(err, &keywordValidation) {
		t.Fatalf("关键词应拒绝他人图片: %v", err)
	}
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if _, err := keywords.Add(ctx, fixture.ownerID, fixture.ownerAccount, keywordsapp.Draft{Keyword: "图", Type: "image", LibraryImageID: fixture.ownerImageID}); err != nil {
		t.Fatalf("关键词应接受自己的图片: %v", err)
	}
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if err := keywords.Replace(ctx, fixture.ownerID, fixture.ownerAccount, []keywordsapp.Draft{{Keyword: "x", Type: "image", LibraryImageID: fixture.otherImageID}}); !errors.As(err, &keywordValidation) {
		t.Fatalf("批量替换应拒绝他人图片: %v", err)
	}

	// default reply：他人图片返回 ErrImageNotFound。
	replies := NewDefaultReplyRepository(store)
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if err := replies.Upsert(ctx, fixture.ownerAccount, defaultreplyapp.Reply{ReplyLibraryImageID: fixture.otherImageID}); !errors.Is(err, defaultreplyapp.ErrImageNotFound) {
		t.Fatalf("默认回复应拒绝他人图片: %v", err)
	}
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if err := replies.Upsert(ctx, fixture.ownerAccount, defaultreplyapp.Reply{Enabled: true, ReplyLibraryImageID: fixture.ownerImageID}); err != nil {
		t.Fatalf("默认回复应接受自己的图片: %v", err)
	}

	// item greeting。
	// greetings 为本处局部值。
	greetings := NewItemGreetingRepository(store)
	// greetingInvalid 为本处局部值。
	var greetingInvalid *itemgreetingapp.InvalidInputError
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if _, err := greetings.Create(ctx, itemgreetingapp.Rule{CookieID: fixture.ownerAccount, Name: "g", ReplyLibraryImageID: fixture.otherImageID, ItemIDs: []string{"i"}}); !errors.As(err, &greetingInvalid) {
		t.Fatalf("首次回复应拒绝他人图片: %v", err)
	}
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if _, err := greetings.Create(ctx, itemgreetingapp.Rule{CookieID: fixture.ownerAccount, Name: "g", ReplyLibraryImageID: fixture.ownerImageID, ItemIDs: []string{"i"}}); err != nil {
		t.Fatalf("首次回复应接受自己的图片: %v", err)
	}

	// cards。
	// cards 为本处局部值。
	cards := NewCardsRepository(store)
	// cardValidation 为本处局部值。
	var cardValidation *cardsapp.ValidationError
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if _, err := cards.Create(ctx, cardsapp.Card{Name: "c", Type: "image", LibraryImageID: fixture.otherImageID, UserID: fixture.ownerID}); !errors.As(err, &cardValidation) {
		t.Fatalf("卡密应拒绝他人图片: %v", err)
	}
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if _, err := cards.Create(ctx, cardsapp.Card{Name: "c", Type: "image", LibraryImageID: fixture.ownerImageID, UserID: fixture.ownerID}); err != nil {
		t.Fatalf("卡密应接受自己的图片: %v", err)
	}

	// delivery template 的图片消息。
	templates := NewDeliveryTemplateRepository(store)
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if _, err := templates.Create(ctx, fixture.ownerID, deliveryapp.Draft{Name: "t", Enabled: true, Messages: []string{deliverytemplate.ImageToken(fixture.otherImageID)}}); !errors.Is(err, deliveryapp.ErrInvalidInput) {
		t.Fatalf("发货模板应拒绝他人图片: %v", err)
	}
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if _, err := templates.Create(ctx, fixture.ownerID, deliveryapp.Draft{Name: "t", Enabled: true, Messages: []string{"文字", deliverytemplate.ImageToken(fixture.ownerImageID)}}); err != nil {
		t.Fatalf("发货模板应接受自己的图片: %v", err)
	}

	// quick reply。
	// chatRepo 为本处局部值。
	chatRepo := chatRepository{store: store}
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if _, err := chatRepo.CreateQuickReplyWithImage(ctx, fixture.ownerAccount, "", fixture.otherImageID); !errors.Is(err, chatapp.ErrQuickReplyImageNotFound) {
		t.Fatalf("快捷回复应拒绝他人图片: %v", err)
	}
	// err 保存被测调用的失败原因，断言时按预期分支检查；reply 是待校验的默认回复输入。
	if reply, err := chatRepo.CreateQuickReplyWithImage(ctx, fixture.ownerAccount, "", fixture.ownerImageID); err != nil || reply.LibraryImageID != fixture.ownerImageID {
		t.Fatalf("快捷回复应接受自己的图片: reply=%+v err=%v", reply, err)
	}

	// 全部写入成功后，图片库删除保护必须发现这些引用。
	usages, err := store.LibraryImages.Usages(ctx, fixture.ownerID)
	if err != nil || len(usages) < 6 {
		t.Fatalf("引用未被全部发现: usages=%+v err=%v", usages, err)
	}
	// refs 为本处局部值；refsErr 为本处局部值。
	refs, refsErr := NewLibraryImageRepository(store).TemplateImageRefs(ctx, fixture.ownerID)
	if refsErr != nil || len(refs) < 6 {
		t.Fatalf("删除保护引用不全: refs=%+v err=%v", refs, refsErr)
	}
}
