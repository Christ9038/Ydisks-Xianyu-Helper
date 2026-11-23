package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// internalErrorWriters 是写统一错误响应的函数名；其中 5xx 响应所在分支必须有日志记录底层错误。
var internalErrorWriters = map[string]bool{"writeErr": true, "writeErrRequest": true, "writeErrCode": true, "writeErrDetails": true}

// serverErrorStatuses 是需要留下日志的服务端故障状态常量名。
var serverErrorStatuses = map[string]bool{"StatusInternalServerError": true, "StatusBadGateway": true, "StatusServiceUnavailable": true, "StatusGatewayTimeout": true}

// internalErrorGuardExemptions 是确认没有底层错误可记录的 5xx 响应，键为“文件:函数名”。
// 新增条目必须说明理由；有底层错误的响应不得加入，应改用 writeInternalErr 或 logInternalFailure。
var internalErrorGuardExemptions = map[string]string{}

// isLoggingCall 判断调用是否会记录日志：slog、logging 包、记录器方法、logInternalFailure/logChatFailure 与 writeInternalErr*。
func isLoggingCall(call *ast.CallExpr) bool {
	// callee 是被调用表达式，按标识符或选择器两种形态分别判断。
	switch callee := call.Fun.(type) {
	case *ast.Ident:
		return callee.Name == "logInternalFailure" || callee.Name == "logChatFailure" || strings.HasPrefix(callee.Name, "writeInternalErr")
	case *ast.SelectorExpr:
		// owner 是被调用方法所属的标识符或字段名，例如 slog、logging、s.Logger 中的 Logger。
		owner := ""
		// receiver 是选择器左侧的接收者表达式，用来取出记录器的名字。
		switch receiver := callee.X.(type) {
		case *ast.Ident:
			owner = receiver.Name
		case *ast.SelectorExpr:
			owner = receiver.Sel.Name
		}
		return owner == "slog" || owner == "logging" || owner == "Logger" || owner == "logger" || strings.HasPrefix(callee.Sel.Name, "writeInternalErr")
	}
	return false
}

// hasErrorIdent 判断语法子树是否引用了名为 err 或以 Err 结尾的标识符。
func hasErrorIdent(node ast.Node) bool {
	// found 记录是否发现错误变量，发现后停止遍历。
	found := false
	ast.Inspect(node, func(visited ast.Node) bool {
		// ident、isIdent 是选择器的“Sel 名称”节点及其类型断言结果。
		if ident, isIdent := visited.(*ast.Ident); isIdent && (ident.Name == "err" || strings.HasSuffix(ident.Name, "Err")) {
			found = true
		}
		return !found
	})
	return found
}

// containsLogging 判断语法子树内是否含日志调用。
func containsLogging(node ast.Node) bool {
	// logged 记录是否发现日志调用，发现后停止遍历。
	logged := false
	ast.Inspect(node, func(visited ast.Node) bool {
		// call、isCall 是遍历节点转换为调用表达式的结果及是否成功。
		if call, isCall := visited.(*ast.CallExpr); isCall && isLoggingCall(call) {
			logged = true
		}
		return !logged
	})
	return logged
}

// callHasServerErrorStatus 判断调用的参数里是否带有服务端故障状态码常量。
func callHasServerErrorStatus(call *ast.CallExpr) bool {
	// arg 是调用的单个实参表达式。
	for _, arg := range call.Args {
		// selector 是形如 http.StatusBadGateway 的状态码常量引用。
		if selector, isSelector := arg.(*ast.SelectorExpr); isSelector && serverErrorStatuses[selector.Sel.Name] {
			return true
		}
	}
	return false
}

// inErrorBranch 判断响应调用是否处于与错误有关的分支：参数引用错误变量，或祖先 if/switch 的条件判断了错误。
// stack 是从函数体到该调用的祖先节点链，最后一个元素是调用本身。
func inErrorBranch(stack []ast.Node) bool {
	// call 是被检查的响应调用；只检查参数，避免函数名 writeErr 本身被当成错误变量。
	call := stack[len(stack)-1].(*ast.CallExpr)
	// arg 是响应调用的单个实参，只在实参里查找错误变量。
	for _, arg := range call.Args {
		if hasErrorIdent(arg) {
			return true
		}
	}
	// ancestor 是从外到内的祖先节点，用于读取包围响应的 if/case/switch 条件。
	for _, ancestor := range stack[:len(stack)-1] {
		// typed 是按祖先节点具体类型断言后的值，用于读取其条件字段。
		switch typed := ancestor.(type) {
		case *ast.IfStmt:
			if (typed.Init != nil && hasErrorIdent(typed.Init)) || hasErrorIdent(typed.Cond) {
				return true
			}
		case *ast.CaseClause:
			// condition 是 case 分支的单个匹配条件表达式。
			for _, condition := range typed.List {
				if hasErrorIdent(condition) {
					return true
				}
			}
		case *ast.SwitchStmt:
			if typed.Tag != nil && hasErrorIdent(typed.Tag) {
				return true
			}
			// 同一个 switch 的 default 分支没有条件，需看同级 case 是否判断了错误。
			for _, clause := range typed.Body.List {
				// caseClause 是该 switch 的一个分支。
				caseClause, isCase := clause.(*ast.CaseClause)
				if !isCase {
					continue
				}
				// condition 是同级 case 分支的单个匹配条件表达式。
				for _, condition := range caseClause.List {
					if hasErrorIdent(condition) {
						return true
					}
				}
			}
		}
	}
	return false
}

// loggedBefore 判断响应所在的任一语句序列中，位于响应之前是否已有日志（直接日志语句，或紧邻的前一条语句内含日志）。
// stack 是从函数体到该调用的祖先节点链。
func loggedBefore(stack []ast.Node) bool {
	// index 是祖先在链中的位置，ancestor 是该祖先节点本身。
	for index, ancestor := range stack[:len(stack)-1] {
		// statements 是该祖先节点的直接语句序列；非语句容器为空。
		var statements []ast.Stmt
		// typed 是按祖先节点具体类型断言后的值，用于取出其直接语句序列。
		switch typed := ancestor.(type) {
		case *ast.BlockStmt:
			statements = typed.List
		case *ast.CaseClause:
			statements = typed.Body
		}
		// child 是通向响应调用的下一个祖先节点。
		child := stack[index+1]
		// previous 是紧邻响应所在语句之前的那条语句，例如 if s.Logger != nil { ... }。
		var previous ast.Stmt
		// statement 是响应所在语句之前的单条语句。
		for _, statement := range statements {
			if statement == child {
				break
			}
			// exprStatement 是形如 logInternalFailure(...) 的独立调用语句。
			if exprStatement, isExpr := statement.(*ast.ExprStmt); isExpr {
				// logCall、isCall 是独立调用语句里的调用表达式及是否为调用。
				if logCall, isCall := exprStatement.X.(*ast.CallExpr); isCall && isLoggingCall(logCall) {
					return true
				}
			}
			previous = statement
		}
		if previous != nil && containsLogging(previous) {
			return true
		}
	}
	return false
}

// TestServerErrorResponsesLogUnderlyingError 守卫：错误分支里的 5xx 响应之前必须记录底层错误，否则线上只剩固定文案无法排查。
func TestServerErrorResponsesLogUnderlyingError(t *testing.T) {
	// files 是 internal/server 下的全部源文件。
	files, globErr := filepath.Glob("*.go")
	if globErr != nil {
		t.Fatal(globErr)
	}
	// violations 保存违规位置，排序后输出便于定位。
	var violations []string
	// file 是被扫描的生产源文件相对路径。
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		// fset、parsed 是该文件的位置集合与语法树。
		fset := token.NewFileSet()
		// parsed、parseErr 是该文件的语法树及解析错误。
		parsed, parseErr := parser.ParseFile(fset, file, nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		// declaration 是文件内的单个顶层声明。
		for _, declaration := range parsed.Decls {
			// function 是顶层函数声明；只检查带函数体且引用了错误变量的函数。
			function, isFunc := declaration.(*ast.FuncDecl)
			if !isFunc || function.Body == nil || !hasErrorIdent(function) {
				continue
			}
			// stack 保存遍历时的祖先节点链。
			var stack []ast.Node
			ast.Inspect(function.Body, func(visited ast.Node) bool {
				if visited == nil {
					stack = stack[:len(stack)-1]
					return true
				}
				stack = append(stack, visited)
				// call 是当前遍历到的调用表达式。
				call, isCall := visited.(*ast.CallExpr)
				if !isCall {
					return true
				}
				// writer 是被调用的函数标识符。
				writer, isIdent := call.Fun.(*ast.Ident)
				if !isIdent || !internalErrorWriters[writer.Name] || !callHasServerErrorStatus(call) {
					return true
				}
				if !inErrorBranch(stack) || loggedBefore(stack) {
					return true
				}
				// key 是豁免表使用的“文件:函数名”。
				key := file + ":" + function.Name.Name
				// exempt 表示该函数是否在确认无底层错误的豁免表中。
				if _, exempt := internalErrorGuardExemptions[key]; !exempt {
					violations = append(violations, fset.Position(call.Pos()).String()+" "+function.Name.Name+" 的 5xx 响应处于错误分支但此前没有日志")
				}
				return true
			})
		}
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("以下 5xx 响应没有记录底层错误（请用 writeInternalErr / logInternalFailure）：\n%s", strings.Join(violations, "\n"))
	}
}
