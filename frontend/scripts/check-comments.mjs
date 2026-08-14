// check-comments 检查 TypeScript/TSX 声明是否带有准确的中文注释。
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import ts from "typescript";

// HAN_PATTERN 用于识别至少包含一个汉字的注释文本。
const HAN_PATTERN = /\p{Script=Han}/u;

// parseArguments 解析注释门禁所需的少量命令行参数。
function parseArguments(argv) {
  // options 保存命令行参数及其默认值。
  const options = {
    mode: "check",
    root: ".",
    baseline: ".commentlint/frontend-baseline.json",
  };
  for (let index = 0; index < argv.length; index += 1) {
    // argument 是当前处理的命令行参数。
    const argument = argv[index];
    if (argument === "--mode" || argument === "--root" || argument === "--baseline") {
      // value 是当前选项紧随其后的值。
      const value = argv[index + 1];
      if (!value) {
        throw new Error(`${argument} 缺少参数值`);
      }
      options[argument.slice(2)] = value;
      index += 1;
    }
  }
  if (options.mode !== "check" && options.mode !== "baseline") {
    throw new Error(`不支持的模式：${options.mode}`);
  }
  return options;
}

// collectSourceFiles 递归找到需要检查的 TypeScript 和 TSX 源文件。
function collectSourceFiles(rootDirectory) {
  // sourceFiles 保存按路径排序后的源码文件。
  const sourceFiles = [];
  // visit 递归遍历单个目录并跳过依赖与构建产物。
  function visit(directory) {
    // entries 是当前目录的文件系统条目。
    const entries = fs.readdirSync(directory, { withFileTypes: true });
    for (const entry of entries) {
      // absolutePath 是当前条目的绝对路径。
      const absolutePath = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        if (["node_modules", "dist", "coverage", "generated"].includes(entry.name)) {
          continue;
        }
        visit(absolutePath);
        continue;
      }
      if (entry.isFile() && /\.(ts|tsx)$/.test(entry.name) && !entry.name.endsWith(".d.ts")) {
        sourceFiles.push(absolutePath);
      }
    }
  }
  visit(rootDirectory);
  return sourceFiles.sort();
}

// getCommentAnchor 返回声明对应的语句节点，确保变量前的注释能够覆盖每个声明项。
function getCommentAnchor(node) {
  if (ts.isVariableDeclaration(node)) {
    // variableStatement 是变量声明所在的完整语句节点。
    const variableStatement = node.parent?.parent;
    if (variableStatement && ts.isVariableStatement(variableStatement)) {
      return variableStatement;
    }
  }
  return node;
}

// hasChineseComment 判断声明前方或同一行尾部是否存在中文注释。
function hasChineseComment(sourceFile, node) {
  // anchor 是真正承载注释的声明或语句节点。
  const anchor = getCommentAnchor(node);
  // sourceText 是当前文件的原始文本，用于读取注释范围。
  const sourceText = sourceFile.getFullText();
  // startLine 是声明起始位置所在的零基行号。
  const startLine = sourceFile.getLineAndCharacterOfPosition(anchor.getStart(sourceFile)).line;
  // leadingRanges 是声明前方的注释范围。
  const leadingRanges = ts.getLeadingCommentRanges(sourceText, anchor.getFullStart()) ?? [];
  for (const range of leadingRanges) {
    // commentEndLine 是注释末尾所在的零基行号。
    const commentEndLine = sourceFile.getLineAndCharacterOfPosition(range.end).line;
    if (commentEndLine <= startLine && commentEndLine >= startLine - 1 && HAN_PATTERN.test(sourceText.slice(range.pos, range.end))) {
      return true;
    }
  }
  // trailingRanges 允许同一行的行尾注释为短小声明提供说明。
  const trailingRanges = ts.getTrailingCommentRanges(sourceText, anchor.end) ?? [];
  if (trailingRanges.some((range) => {
    // commentStartLine 是行尾注释起始所在的零基行号。
    const commentStartLine = sourceFile.getLineAndCharacterOfPosition(range.pos).line;
    return commentStartLine === startLine && HAN_PATTERN.test(sourceText.slice(range.pos, range.end));
  })) {
    return true;
  }
  // inlinePrefix 是节点前方紧邻的内联注释，用于支持调用参数和类型字面量中的字段注释。
  const inlinePrefix = sourceText.slice(Math.max(0, node.getStart(sourceFile) - 200), node.getStart(sourceFile));
  const prefixMatch = inlinePrefix.match(/(?:\/\*[\s\S]*?\*\/|\/\/[^\n]*)\s*$/);
  if (prefixMatch && HAN_PATTERN.test(prefixMatch[0])) {
    return true;
  }
  // inlineSuffix 是节点结束位置紧邻的内联注释，用于支持循环变量和行尾字段注释。
  const inlineSuffix = sourceText.slice(node.end, Math.min(sourceText.length, node.end + 200));
  const suffixMatch = inlineSuffix.match(/^\s*(?:\/\*[\s\S]*?\*\/|\/\/[^\n]*)/);
  if (suffixMatch && HAN_PATTERN.test(suffixMatch[0])) {
    return true;
  }
  // inlineBody 是箭头函数主体开头的注释，用于保持 JSX/源码关键片段可读且不打断表达式前缀。
  const inlineBody = sourceText.slice(node.getStart(sourceFile), Math.min(sourceText.length, node.getStart(sourceFile) + 120));
  const bodyMatch = inlineBody.match(/=>[\s\S]{0,80}?\/\*[\s\S]*?\*\//);
  return Boolean(bodyMatch && HAN_PATTERN.test(bodyMatch[0]));
}

// getNodeName 返回 AST 节点的可读名称。
function getNodeName(node) {
  if (node.name) {
    return node.name.getText();
  }
  if (ts.isParameter(node) && node.name) {
    return node.name.getText();
  }
  return "anonymous";
}

// findingFor 将一个 AST 节点转换成稳定的基线记录。
function findingFor(relativePath, sourceFile, node, kind) {
  // line 是声明所在的一基行号。
  const line = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile)).line + 1;
  return { file: relativePath, line, kind, name: getNodeName(node) };
}

// inspectSourceFile 遍历一个文件并收集缺少中文注释的声明。
function inspectSourceFile(rootDirectory, filePath) {
  // sourceText 是待解析文件内容。
  const sourceText = fs.readFileSync(filePath, "utf8");
  // sourceFile 是开启父节点信息的 TypeScript AST。
  const sourceFile = ts.createSourceFile(filePath, sourceText, ts.ScriptTarget.Latest, true, filePath.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  // relativePath 使基线在不同机器上保持可复用。
  const relativePath = path.relative(rootDirectory, filePath).split(path.sep).join("/");
  // findings 保存当前文件的注释问题。
  const findings = [];
  // visit 递归检查支持注释的声明节点。
  function visit(node) {
    // shouldCheckArrow 表示当前函数表达式没有被变量声明注释覆盖。
    const shouldCheckArrow = (ts.isArrowFunction(node) || ts.isFunctionExpression(node)) && !ts.isVariableDeclaration(node.parent);
    if (ts.isVariableDeclaration(node) && !hasChineseComment(sourceFile, node)) {
      findings.push(findingFor(relativePath, sourceFile, node, "variable"));
    } else if (ts.isFunctionDeclaration(node) && !hasChineseComment(sourceFile, node)) {
      findings.push(findingFor(relativePath, sourceFile, node, "function"));
    } else if ((ts.isMethodDeclaration(node) || ts.isMethodSignature(node) || ts.isPropertyDeclaration(node) || ts.isPropertySignature(node)) && !hasChineseComment(sourceFile, node)) {
      findings.push(findingFor(relativePath, sourceFile, node, "member"));
    } else if (shouldCheckArrow && !hasChineseComment(sourceFile, node)) {
      findings.push(findingFor(relativePath, sourceFile, node, "function-expression"));
    }
    ts.forEachChild(node, visit);
  }
  visit(sourceFile);
  return findings;
}

// findingKey 生成历史基线中的稳定键。
function findingKey(finding) {
  return `${finding.file}:${finding.line}:${finding.kind}:${finding.name}`;
}

// sortFindings 按文件、行号、类别和名称对结果排序。
function sortFindings(findings) {
  return findings.sort((left, right) => findingKey(left).localeCompare(findingKey(right)));
}

// writeBaseline 写入缩进格式的前端历史问题基线。
function writeBaseline(filePath, findings) {
  // parentDirectory 是基线文件所属目录。
  const parentDirectory = path.dirname(filePath);
  fs.mkdirSync(parentDirectory, { recursive: true });
  fs.writeFileSync(filePath, `${JSON.stringify(findings, null, 2)}\n`, "utf8");
}

// readBaseline 读取历史问题并建立快速查询集合。
function readBaseline(filePath) {
  // baselineText 是基线文件的 JSON 文本。
  const baselineText = fs.readFileSync(filePath, "utf8");
  // baselineFindings 是反序列化后的历史问题数组。
  const baselineFindings = JSON.parse(baselineText);
  return new Set(baselineFindings.map(findingKey));
}

// main 执行前端源码扫描、基线生成或新增问题检查。
function main() {
  // options 保存解析后的命令行选项。
  const options = parseArguments(process.argv.slice(2));
  // rootDirectory 是规范化后的源码根目录。
  const rootDirectory = path.resolve(options.root);
  // baselinePath 是规范化后的历史基线路径。
  const baselinePath = path.resolve(options.baseline);
  // findings 是当前所有 TypeScript/TSX 文件的检查结果。
  const findings = sortFindings(collectSourceFiles(rootDirectory).flatMap((filePath) => inspectSourceFile(rootDirectory, filePath)));
  if (options.mode === "baseline") {
    writeBaseline(baselinePath, findings);
    console.log(`commentlint: 已记录 ${findings.length} 个历史问题到 ${options.baseline}`);
    return;
  }
  // baselineKeys 保存允许继续存在的历史问题。
  const baselineKeys = readBaseline(baselinePath);
  // newFindings 只包含本次新增或位置变化的问题。
  const newFindings = findings.filter((finding) => !baselineKeys.has(findingKey(finding)));
  for (const finding of newFindings) {
    console.log(`${finding.file}:${finding.line}: [${finding.kind}] ${finding.name} 缺少中文注释`);
  }
  if (newFindings.length > 0) {
    throw new Error(`发现 ${newFindings.length} 个新增问题`);
  }
  console.log(`commentlint: 通过（扫描 ${findings.length} 个历史问题）`);
}

try {
  main();
} catch (error) {
  console.error(`commentlint: ${error instanceof Error ? error.message : String(error)}`);
  process.exitCode = 1;
}
