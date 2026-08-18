package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
)

// checkStageTwoTransportBoundary 扫描 HTTP transport 与入口包的组合根及平台依赖旁路。
func checkStageTwoTransportBoundary(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// normalizedPath 是统一使用斜杠的仓库相对路径。
	normalizedPath := filepath.ToSlash(relativePath)
	if strings.HasSuffix(normalizedPath, "_test.go") {
		return nil
	}
	// isServer 表示当前文件是否属于 HTTP transport 包。
	isServer := strings.HasPrefix(normalizedPath, "internal/server/")
	// isServerCommand 表示当前文件是否属于 Server 进程入口包。
	isServerCommand := strings.HasPrefix(normalizedPath, "cmd/server/")
	if !isServer && !isServerCommand {
		return nil
	}
	// violations 保存当前源码文件违反阶段二边界的全部位置。
	var violations []violation
	if isServer && filepath.Base(normalizedPath) == "application_services.go" {
		violations = append(violations, violation{file: normalizedPath, line: 1, message: "阶段二禁止 internal/server/application_services.go；应用服务、runner 和 coordinator 必须迁入 composition 与应用层"})
	}
	// forbiddenImports 是当前阶段不允许出现在 transport 或 cmd 组合入口的跨层依赖。
	var forbiddenImports map[string]string
	if isServer {
		forbiddenImports = map[string]string{
			"internal/account":    "Server 不得直接依赖 account.Manager，必须消费应用 Port",
			"internal/adapter":    "Server 不得直接依赖 adapter 实现或 factory，必须消费应用 Port",
			"internal/automation": "Server 不得直接依赖 automation.Center，必须消费应用 Port",
			"internal/browser":    "Server 不得直接依赖 browser 实现，必须消费应用 Port",
			"internal/chat":       "Server 不得直接依赖 chat.Service，必须消费聊天应用 Port",
			"internal/db":         "Server 不得直接依赖 db.Store 或 repository，实现必须留在 adapter",
			"internal/notify":     "Server 不得直接依赖 notifier，实现必须留在通知应用 Port",
			"internal/xianyu":     "Server 不得直接依赖平台协议，实现必须留在 adapter",
		}
	} else {
		forbiddenImports = map[string]string{
			"internal/adapter":      "cmd/server 不得直接装配 adapter 服务；必须委托 internal/composition",
			"internal/account":      "cmd/server 不得直接装配账号运行时；必须委托 internal/composition",
			"internal/automation":   "cmd/server 不得直接装配自动化 worker；必须委托 internal/composition",
			"internal/chat":         "cmd/server 不得直接装配聊天服务；必须委托 internal/composition",
			"internal/notify":       "cmd/server 不得直接装配通知 worker；必须委托 internal/composition",
			"internal/application/": "cmd/server 不得直接构造应用服务或 worker；必须委托 internal/composition",
		}
	}
	// imported 是当前源码声明的内部或标准库导入，必须逐项检查其是否越过当前层边界。
	for _, imported := range syntax.Imports {
		// rawPath、unquoteErr 分别是当前导入路径及其语法解析错误。
		rawPath, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil {
			continue
		}
		// importPath 是去除模块前缀后的稳定内部导入路径。
		importPath := normalizeImportPath(rawPath)
		// forbiddenPath 是禁止导入的内部路径前缀；message 是命中后返回给迁移者的具体修复方向。
		for forbiddenPath, message := range forbiddenImports {
			if importPath == forbiddenPath || strings.HasPrefix(importPath, forbiddenPath) {
				violations = append(violations, violation{file: normalizedPath, line: fset.Position(imported.Pos()).Line, message: message})
				break
			}
		}
	}
	if isServer {
		violations = append(violations, checkStageTwoServerDeclarations(normalizedPath, syntax, fset)...)
		violations = append(violations, checkStageTwoServerConstruction(normalizedPath, syntax, fset)...)
		violations = append(violations, checkStageTwoApplicationPortDeclarations(normalizedPath, syntax, fset)...)
	}
	violations = append(violations, checkStageTwoCompositionProjection(normalizedPath, syntax, fset)...)
	if isServerCommand {
		violations = append(violations, checkStageTwoCmdCalls(normalizedPath, syntax, fset)...)
	}
	return violations
}

// checkStageTwoApplicationPortDeclarations 拒绝 HTTP Port 容器重新持有具体应用服务实现。
func checkStageTwoApplicationPortDeclarations(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	if filepath.Base(relativePath) != "application_ports.go" {
		return nil
	}
	// applicationAliases 保存当前文件中应用包的本地导入别名。
	applicationAliases := applicationImportAliases(syntax)
	// violations 保存具体应用服务指针泄露到 transport Port 容器的全部位置。
	var violations []violation
	// declaration 是当前文件的顶级声明，只有类型声明可能定义 Port 容器。
	for _, declaration := range syntax.Decls {
		// generalDeclaration、ok 分别是转换后的通用声明及其类型匹配状态。
		generalDeclaration, ok := declaration.(*ast.GenDecl)
		if !ok || generalDeclaration.Tok != token.TYPE {
			continue
		}
		// specification 是该通用声明中的单个类型规格。
		for _, specification := range generalDeclaration.Specs {
			// typeSpecification、ok 分别是转换后的类型规格及其匹配状态。
			typeSpecification, ok := specification.(*ast.TypeSpec)
			if !ok || (typeSpecification.Name.Name != "ApplicationPorts" && typeSpecification.Name.Name != "ApplicationPortsInput") {
				continue
			}
			// structure、ok 分别是 Port 容器的结构体定义及其类型匹配状态。
			structure, ok := typeSpecification.Type.(*ast.StructType)
			if !ok {
				continue
			}
			// field 是当前 Port 容器声明的字段，用于检查是否泄露具体应用服务。
			for _, field := range structure.Fields.List {
				if !containsConcreteApplicationServicePointer(field.Type, applicationAliases) {
					continue
				}
				violations = append(violations, violation{file: relativePath, line: fset.Position(field.Pos()).Line, message: "阶段二 HTTP 应用 Port 容器不得持有具体 application Service 指针；请在 internal/server 定义消费者接口并由 composition 投影实现"})
			}
		}
	}
	return violations
}

// applicationImportAliases 收集 internal/application 子包的本地别名。
func applicationImportAliases(syntax *ast.File) map[string]struct{} {
	// aliases 保存应用包在当前文件中的可见名称。
	aliases := make(map[string]struct{})
	// imported 是当前 transport 文件的导入项，用于识别应用包的本地别名。
	for _, imported := range syntax.Imports {
		// rawPath、unquoteErr 分别是导入路径的去引号文本及其解析错误。
		rawPath, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil {
			continue
		}
		// importPath 是标准化后的仓库内导入路径。
		importPath := normalizeImportPath(rawPath)
		if !strings.HasPrefix(importPath, "internal/application/") {
			continue
		}
		// alias 是当前文件使用的应用包本地名称，默认取导入路径末段。
		alias := filepath.Base(importPath)
		if imported.Name != nil {
			alias = imported.Name.Name
		}
		if alias != "_" && alias != "." {
			aliases[alias] = struct{}{}
		}
	}
	return aliases
}

// containsConcreteApplicationServicePointer 递归识别字段类型中隐藏的具体应用服务指针。
func containsConcreteApplicationServicePointer(expression ast.Expr, applicationAliases map[string]struct{}) bool {
	// pointer 表示当前类型是否直接持有应用层对象；只有具体 Service/ServiceSet 才违反 Port 边界。
	if pointer, ok := expression.(*ast.StarExpr); ok {
		// selector、ok 分别是指针元素的限定类型选择器及其匹配状态。
		if selector, ok := pointer.X.(*ast.SelectorExpr); ok {
			// packageName、ok 分别是限定类型所属包标识符及其匹配状态。
			if packageName, ok := selector.X.(*ast.Ident); ok {
				// imported 表示该包名是否来自当前文件导入的应用层包。
				if _, imported := applicationAliases[packageName.Name]; imported && (strings.HasSuffix(selector.Sel.Name, "Service") || strings.HasSuffix(selector.Sel.Name, "ServiceSet")) {
					return true
				}
			}
		}
		return containsConcreteApplicationServicePointer(pointer.X, applicationAliases)
	}
	// array 表示切片或数组字段；递归避免以集合形式隐藏具体服务。
	if array, ok := expression.(*ast.ArrayType); ok {
		return containsConcreteApplicationServicePointer(array.Elt, applicationAliases)
	}
	// mapping 表示 map 字段；键和值都不能用于隐藏服务实例。
	if mapping, ok := expression.(*ast.MapType); ok {
		return containsConcreteApplicationServicePointer(mapping.Key, applicationAliases) || containsConcreteApplicationServicePointer(mapping.Value, applicationAliases)
	}
	return false
}

// checkStageTwoCompositionProjection 确保 composition 核心不反向依赖 Server，只有 runtime 子层可以投影 Server 依赖。
func checkStageTwoCompositionProjection(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	if !strings.HasPrefix(relativePath, "internal/composition/") || strings.HasPrefix(relativePath, "internal/composition/runtime/") {
		return nil
	}
	// imported 是 composition 核心文件的导入项，核心层不得反向导入 Server。
	for _, imported := range syntax.Imports {
		// rawPath、unquoteErr 分别是导入路径的去引号文本及其解析错误。
		rawPath, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil || normalizeImportPath(rawPath) != "internal/server" {
			continue
		}
		return []violation{{file: relativePath, line: fset.Position(imported.Pos()).Line, message: "阶段二 composition 核心不得依赖 Server；只能由 internal/composition/runtime 投影 server.Dependencies"}}
	}
	return nil
}

// checkStageTwoServerDeclarations 禁止 Server 继续声明服务集合、平台 Port 或反向生命周期 API。
func checkStageTwoServerDeclarations(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// forbiddenTypes 是必须从 internal/server 消失的组合根和平台类型。
	forbiddenTypes := map[string]string{
		"ApplicationServices":            "Server 不能拥有应用服务集合；请迁入 composition 并向 handler 注入最小应用 Port",
		"ApplicationServiceDependencies": "Server 不能声明应用服务装配依赖；请迁入 composition",
		"PlatformPort":                   "Server 不能持有 MTOP、长登录或二维码平台 Port；请迁入对应应用用例",
	}
	// forbiddenMethods 是必须从 Server 消失的反向访问器和 session callback。
	forbiddenMethods := map[string]string{
		"ApplicationServices":       "Server 不得向外返还应用服务集合",
		"LifecycleComponents":       "Server 不得生成或返还应用 worker 生命周期组件",
		"mtopClient":                "Server 不得获取 MTOP 客户端",
		"longLoginClient":           "Server 不得获取长登录客户端",
		"qrLoginService":            "Server 不得获取二维码平台客户端",
		"sessionRecoveryCallback":   "Server 不得创建 session recovery callback",
		"recoverExpiredMTOPSession": "Server 不得承载 MTOP session 恢复",
	}
	// violations 保存声明层违反阶段二边界的位置。
	var violations []violation
	// declaration 是当前 Server 文件的顶层声明，可能声明禁止的组合根类型或访问器。
	for _, declaration := range syntax.Decls {
		// typedDeclaration 是顶层声明的具体语法类型，用于分开处理类型和函数声明。
		switch typedDeclaration := declaration.(type) {
		case *ast.GenDecl:
			if typedDeclaration.Tok != token.TYPE {
				continue
			}
			// specification 是当前类型声明块中的单个类型定义。
			for _, specification := range typedDeclaration.Specs {
				// typeSpecification、ok 分别是当前语法规格是否为具名类型及其断言结果。
				typeSpecification, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				// message、forbidden 分别是命中禁止类型时的修复提示及其匹配结果。
				if message, forbidden := forbiddenTypes[typeSpecification.Name.Name]; forbidden {
					violations = append(violations, violation{file: relativePath, line: fset.Position(typeSpecification.Pos()).Line, message: message})
				}
			}
		case *ast.FuncDecl:
			// message、forbidden 分别是命中禁止 Server 访问器时的修复提示及其匹配结果。
			if message, forbidden := forbiddenMethods[typedDeclaration.Name.Name]; forbidden {
				violations = append(violations, violation{file: relativePath, line: fset.Position(typedDeclaration.Pos()).Line, message: message})
			}
		}
	}
	return violations
}

// checkStageTwoServerConstruction 拒绝 Server 直接构造应用服务、runner、coordinator 或 adapter factory。
func checkStageTwoServerConstruction(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// internalAliases 保存当前文件中指向 application 或 adapter 的导入别名，供构造调用精确判定。
	internalAliases := internalConstructionImportAliases(syntax)
	// violations 保存调用层违反阶段二边界的位置。
	var violations []violation
	ast.Inspect(syntax, func(node ast.Node) bool {
		// call 是当前待检查的函数或方法调用。
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		// selector 表示当前调用的接收者与方法名称。
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// name 是当前被调用的方法或构造函数名称。
		name := selector.Sel.Name
		if name == "NewApplicationServices" || name == "LifecycleComponents" || name == "ApplicationServices" || name == "sessionRecoveryCallback" || name == "recoverExpiredMTOPSession" || (strings.HasPrefix(name, "New") && isInternalConstructionCall(selector.X, internalAliases)) {
			violations = append(violations, violation{file: relativePath, line: fset.Position(call.Pos()).Line, message: fmt.Sprintf("Server 禁止调用 %s；应用服务、runner、coordinator 和 adapter factory 必须由 composition 构造", name)})
		}
		return true
	})
	return violations
}

// internalConstructionImportAliases 收集 application 与 adapter 导入的本地别名，避免误判标准库 New 调用。
func internalConstructionImportAliases(syntax *ast.File) map[string]struct{} {
	// aliases 保存需要受阶段二构造门禁保护的本地导入名。
	aliases := make(map[string]struct{})
	// imported 是当前待解析别名的导入声明。
	for _, imported := range syntax.Imports {
		// rawPath、unquoteErr 分别是当前导入路径及其语法解析错误。
		rawPath, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil {
			continue
		}
		// importPath 是去除模块前缀后的内部导入路径。
		importPath := normalizeImportPath(rawPath)
		if !strings.HasPrefix(importPath, "internal/application/") && !strings.HasPrefix(importPath, "internal/adapter") {
			continue
		}
		// alias 是源码调用选择器使用的包名；显式别名优先于路径末段。
		alias := filepath.Base(importPath)
		if imported.Name != nil {
			alias = imported.Name.Name
		}
		if alias != "_" && alias != "." {
			aliases[alias] = struct{}{}
		}
	}
	return aliases
}

// isInternalConstructionCall 判断构造调用是否来自应用/adapter 包或依赖 factory 链。
func isInternalConstructionCall(receiver ast.Expr, internalAliases map[string]struct{}) bool {
	// identifier 是直接包名调用，例如 orderapp.NewRefreshJobRunner。
	if identifier, ok := receiver.(*ast.Ident); ok {
		// found 表示该直接接收者是否是 application 或 adapter 的本地导入别名。
		_, found := internalAliases[identifier.Name]
		return found
	}
	// selector 表示依赖 factory 链，例如 dependencies.ItemDependencies.NewItemBatchRepository。
	_, isFactoryChain := receiver.(*ast.SelectorExpr)
	return isFactoryChain
}

// checkStageTwoCmdCalls 拒绝 cmd/server 通过 Server API 或 factory 间接重新承担业务组合根职责。
func checkStageTwoCmdCalls(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// violations 保存入口层违反阶段二边界的位置。
	var violations []violation
	ast.Inspect(syntax, func(node ast.Node) bool {
		// call 是当前待检查的函数或方法调用。
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		// selector 表示当前调用的接收者与名称。
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// name 是被调用的构造或生命周期访问器名称。
		name := selector.Sel.Name
		if name == "NewApplicationServices" || name == "LifecycleComponents" || name == "ApplicationServices" {
			violations = append(violations, violation{file: relativePath, line: fset.Position(call.Pos()).Line, message: fmt.Sprintf("cmd/server 禁止通过 %s 使用 Server 组合根；必须调用 internal/composition", name)})
		}
		return true
	})
	return violations
}

// checkServerInfrastructureFields 禁止 HTTP Server 保存业务运行时或具体平台实现，避免 transport 成为组合根。
func checkServerInfrastructureFields(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// normalizedPath 是统一使用斜杠的仓库相对路径。
	normalizedPath := filepath.ToSlash(relativePath)
	if !strings.HasPrefix(normalizedPath, "internal/server/") || strings.HasSuffix(normalizedPath, "_test.go") {
		return nil
	}
	// forbiddenTypes 记录 Server 不得作为字段持有者的业务运行时和平台实现类型。
	forbiddenTypes := map[string]string{
		"account.Manager":              "账号 Manager 必须由进程组合根和应用 Port 持有，Server 只能调用应用服务",
		"automation.Center":            "自动化 Center 必须由应用层持有，Server 不能保存业务 worker",
		"notify.Notifier":              "通知器必须通过通知应用 Port 使用，Server 不能保存基础设施实现",
		"adapter.PlatformDependencies": "具体平台依赖必须在组合根封装为消费者定义的 Port",
		"adapter.MTOPClient":           "MTOP 客户端必须通过应用 Port 或不可变平台 Port 提供",
		"adapter.LongLoginClient":      "长登录客户端必须通过应用 Port 或不可变平台 Port 提供",
		"adapter.QRLoginService":       "二维码客户端必须通过应用 Port 或不可变平台 Port 提供",
	}
	// violations 保存 Server 结构体字段违反基础设施持有边界的扫描结果。
	var violations []violation
	// declaration 是当前文件中的类型声明，只有 Server 结构体需要本规则检查。
	for _, declaration := range syntax.Decls {
		// generalDeclaration 表示可能包含类型定义的声明。
		generalDeclaration, ok := declaration.(*ast.GenDecl)
		if !ok || generalDeclaration.Tok != token.TYPE {
			continue
		}
		// specification 是当前声明中的一个类型定义。
		for _, specification := range generalDeclaration.Specs {
			// typeSpecification 表示具体类型名及其语法树。
			typeSpecification, ok := specification.(*ast.TypeSpec)
			if !ok || typeSpecification.Name.Name != "Server" {
				continue
			}
			// structure 表示 Server 的字段集合；非结构体类型不适用本规则。
			structure, ok := typeSpecification.Type.(*ast.StructType)
			if !ok {
				continue
			}
			// field 是当前待检查的 Server 字段。
			for _, field := range structure.Fields.List {
				// typeName 是去掉指针层后的限定类型名；未知表达式保持空字符串。
				typeName := qualifiedTypeName(field.Type)
				// message、forbidden 分别表示字段类型的禁止原因及其是否命中。
				message, forbidden := forbiddenTypes[typeName]
				if !forbidden {
					continue
				}
				violations = append(violations, violation{file: normalizedPath, line: fset.Position(field.Pos()).Line, message: message})
			}
		}
	}
	return violations
}

// qualifiedTypeName 返回指针、选择器或标识符类型的稳定文本名，供结构体字段边界规则使用。
func qualifiedTypeName(expression ast.Expr) string {
	// pointer 表示字段是否以指针形式声明；边界规则不区分值与指针持有。
	if pointer, ok := expression.(*ast.StarExpr); ok {
		return qualifiedTypeName(pointer.X)
	}
	// selector 表示包限定类型，例如 adapter.MTOPClient。
	if selector, ok := expression.(*ast.SelectorExpr); ok {
		// packageName、ok 分别表示选择器左侧是否为可解析的包标识符。
		packageName, ok := selector.X.(*ast.Ident)
		if ok {
			return packageName.Name + "." + selector.Sel.Name
		}
	}
	// identifier 表示当前字段是否为未限定类型；本规则当前不禁止未限定类型。
	if identifier, ok := expression.(*ast.Ident); ok {
		return identifier.Name
	}
	return ""
}

// checkServerCompositionCalls 禁止已迁出的应用 worker 组合逻辑回流到 Server transport 包。
func checkServerCompositionCalls(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// normalizedPath 是统一使用斜杠的仓库相对路径。
	normalizedPath := filepath.ToSlash(relativePath)
	if !strings.HasPrefix(normalizedPath, "internal/server/") || strings.HasSuffix(normalizedPath, "_test.go") {
		return nil
	}
	// forbiddenConstructors 记录必须由进程组合根创建的应用 worker 构造函数及修复提示。
	forbiddenConstructors := map[string]string{
		"NewReconciliationRecoveryCoordinator": "订单补偿恢复协调器必须由 cmd 组合根构造后注入 Server",
		"NewDatabaseHealth":                    "数据库健康检查端口必须由 cmd 组合根构造后注入 Server",
	}
	// forbiddenMethods 记录 Server 不得再作为应用 worker 生命周期反向提供者的遗留方法。
	forbiddenMethods := map[string]string{
		"ApplicationLifecycleComponents": "应用 worker 生命周期组件必须由组合根的应用服务集合登记，Server 不得反向返还组件",
	}
	// violations 保存当前 Server 文件中发现的组合根回流问题。
	var violations []violation
	ast.Inspect(syntax, func(node ast.Node) bool {
		// call 是当前待检查的函数调用节点。
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		// selector 是包函数或对象方法调用；仅包级构造函数可能违反本规则。
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// message 是命中构造函数或遗留生命周期方法后返回的组合根迁移提示。
		message, forbidden := forbiddenConstructors[selector.Sel.Name]
		if !forbidden {
			message, forbidden = forbiddenMethods[selector.Sel.Name]
		}
		if forbidden {
			violations = append(violations, violation{
				file:    normalizedPath,
				line:    fset.Position(call.Pos()).Line,
				message: message,
			})
		}
		return true
	})
	return violations
}

// checkHTTPRequestContracts 禁止 Server handler 使用匿名请求结构，保证请求契约能够被复用、审计和版本化。
func checkHTTPRequestContracts(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// normalizedPath 是统一使用斜杠的仓库相对路径。
	normalizedPath := filepath.ToSlash(relativePath)
	if !strings.HasPrefix(normalizedPath, "internal/server/") || strings.HasSuffix(normalizedPath, "_test.go") || !strings.HasSuffix(normalizedPath, "_handlers.go") {
		return nil
	}
	// violations 保存当前 handler 文件中发现的匿名请求结构。
	var violations []violation
	ast.Inspect(syntax, func(node ast.Node) bool {
		// declaration 是函数体内可能包含请求变量的声明语句。
		declaration, ok := node.(*ast.DeclStmt)
		if !ok {
			return true
		}
		// generalDeclaration 是具体的 var 声明；短变量声明不会承载匿名 struct 类型。
		generalDeclaration, ok := declaration.Decl.(*ast.GenDecl)
		if !ok || generalDeclaration.Tok != token.VAR {
			return true
		}
		// specification 是当前 var 声明中的单个语法规格。
		for _, specification := range generalDeclaration.Specs {
			// valueSpecification 是当前 var 声明的名称、类型和初始值组合。
			valueSpecification, ok := specification.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// anonymousStruct 表示该变量是否直接声明为匿名 struct 类型。
			_, anonymousStruct := valueSpecification.Type.(*ast.StructType)
			if !anonymousStruct {
				continue
			}
			// name 是当前匿名结构声明中的变量名。
			for _, name := range valueSpecification.Names {
				if name.Name != "req" && name.Name != "input" {
					continue
				}
				violations = append(violations, violation{
					file:    normalizedPath,
					line:    fset.Position(valueSpecification.Pos()).Line,
					message: fmt.Sprintf("HTTP 请求变量 %s 禁止使用匿名 struct，请定义具名 DTO", name.Name),
				})
			}
		}
		return true
	})
	return violations
}

// checkRuntimeSetterCalls 禁止生产代码调用仅为测试替身保留的 Adapter 运行时 setter。
func checkRuntimeSetterCalls(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// normalizedPath 是统一使用斜杠的仓库相对路径。
	normalizedPath := filepath.ToSlash(relativePath)
	if strings.HasSuffix(normalizedPath, "_test.go") {
		return nil
	}
	// testOnlySetters 是已登记为测试隔离入口的 Adapter setter 名称。
	testOnlySetters := map[string]struct{}{
		"SetAutomation": {}, "SetNotifier": {}, "SetChatService": {}, "SetCredentialWakeService": {},
		"SetBrowser": {}, "SetRenewService": {}, "SetTokenCaptchaRequester": {}, "SetOrderDetailClient": {},
	}
	// violations 保存生产代码绕过构造期依赖固定的调用位置。
	var violations []violation
	ast.Inspect(syntax, func(node ast.Node) bool {
		// call 是当前待判断的函数调用节点。
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		// selector 是当前调用的选择器表达式，只有明确的 Adapter setter 名称才属于本规则。
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// registered 表示当前调用是否属于明确登记的测试兼容 setter。
		if _, registered := testOnlySetters[selector.Sel.Name]; !registered {
			return true
		}
		violations = append(violations, violation{
			file:    normalizedPath,
			line:    fset.Position(call.Pos()).Line,
			message: fmt.Sprintf("生产代码禁止调用测试兼容 setter %s，请通过构造期 RuntimeBundle 注入", selector.Sel.Name),
		})
		return true
	})
	return violations
}

// checkHTTPResponseContracts 检查 Server 对外响应是否使用具名 DTO，并阻止动态 map 绕过契约边界。
func checkHTTPResponseContracts(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// normalizedPath 是统一使用斜杠的仓库相对路径。
	normalizedPath := filepath.ToSlash(relativePath)
	if !strings.HasPrefix(normalizedPath, "internal/server/") || strings.HasSuffix(normalizedPath, "_test.go") {
		return nil
	}
	// violations 保存当前 Server 文件发现的 HTTP 契约问题。
	var violations []violation
	ast.Inspect(syntax, func(node ast.Node) bool {
		// typedNode 是当前遍历到的 AST 节点，用于识别 HTTP 契约声明或调用。
		switch typedNode := node.(type) {
		case *ast.TypeSpec:
			if !isHTTPContractTypeName(typedNode.Name.Name) {
				return true
			}
			if (typedNode.Name.Name == "settingsResponse" || containsDynamicMapType(typedNode.Type)) &&
				!isControlledDynamicResponseType(typedNode.Name.Name) {
				violations = append(violations, violation{
					file:    normalizedPath,
					line:    fset.Position(typedNode.Pos()).Line,
					message: fmt.Sprintf("HTTP 契约类型 %s 禁止使用动态 map，请定义具名 DTO 字段", typedNode.Name.Name),
				})
			}
		case *ast.CallExpr:
			if !isWriteJSONCall(typedNode) || len(typedNode.Args) < 3 {
				return true
			}
			// responseArg 是 writeJSON 的响应值参数，必须是具名 DTO 或受控类型。
			responseArg := typedNode.Args[2]
			if isDynamicMapLiteral(responseArg) {
				violations = append(violations, violation{
					file:    normalizedPath,
					line:    fset.Position(responseArg.Pos()).Line,
					message: "HTTP 响应禁止直接写入动态 map，请使用具名 DTO",
				})
			}
		}
		return true
	})
	return violations
}

// isControlledDynamicResponseType 判断已登记的动态键兼容响应，避免在未满足 Sunset 条件前改变旧客户端 JSON 形状。
func isControlledDynamicResponseType(name string) bool {
	// ok 表示响应类型是否已在带 Sunset 条件的兼容登记表中备案。
	_, ok := controlledDynamicResponses[name]
	return ok
}

// isForbiddenHiddenDependencyImport 禁止应用与 Server 通过反射、插件机制或动态加载隐藏必需依赖。
func isForbiddenHiddenDependencyImport(filePath, importedPath string) bool {
	// productionLayer 表示必须封死隐式装配旁路的生产层。
	productionLayer := (strings.HasPrefix(filePath, "internal/application/") && !strings.HasSuffix(filePath, "_test.go")) ||
		(strings.HasPrefix(filePath, "internal/server/") && !strings.HasSuffix(filePath, "_test.go"))
	if !productionLayer {
		return false
	}
	for _, forbidden /* forbidden 是禁止隐藏依赖实现的标准库或运行时包名。 */ := range []string{"reflect", "plugin", "unsafe"} {
		if importedPath == forbidden || strings.HasPrefix(importedPath, forbidden+"/") {
			return true
		}
	}
	return false
}
