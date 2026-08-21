// 校验生成类型未被人工修改，并把临时生成结果与仓库产物逐字节比较。
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import ts from 'typescript';

// frontendRoot 是前端源码根目录，所有前端契约和静态门禁均以它解析相对路径。
const frontendRoot = path.resolve(import.meta.dirname, '..');
// repositoryRoot 是包含 OpenAPI 单一契约源的仓库根目录。
const repositoryRoot = path.resolve(frontendRoot, '..');
// temporaryDirectory 保存本次生成的临时 TypeScript 契约，校验完成后必须删除。
const temporaryDirectory = fs.mkdtempSync(path.join(os.tmpdir(), 'ydisks-openapi-'));
// temporarySchema 是临时 OpenAPI 生成文件的绝对路径。
const temporarySchema = path.join(temporaryDirectory, 'schema.ts');
// checkedInSchema 是仓库中受版本控制的 OpenAPI 生成文件。
const checkedInSchema = path.join(frontendRoot, 'shared', 'api-contract', 'generated', 'schema.ts');
// generator 是固定依赖安装后实际执行的 OpenAPI TypeScript 生成器。
const generator = path.join(frontendRoot, 'node_modules', 'openapi-typescript', 'bin', 'cli.js');
// featureRoot 是允许通过 feature adapter 写入系统设置的源码树。
const featureRoot = path.join(frontendRoot, 'app', 'features');
// systemSettingsPath 是必须经由秘密归一器构造请求体的版本化系统设置写入端点。
const systemSettingsPath = '/api/v1/settings/system';

// listFeatureSourceFiles 递归收集 feature 内需要接受系统设置写入门禁的生产 TypeScript 文件。
const listFeatureSourceFiles = directory => {
  // entries 保存当前目录的子项，用于继续递归或收集源码文件。
  const entries = fs.readdirSync(directory, { withFileTypes: true });
  // files 保存当前目录及其后代目录中待检查的生产源码绝对路径。
  const files = [];
  for (const entry of entries) {
    // entryPath 是当前目录项的绝对路径。
    const entryPath = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...listFeatureSourceFiles(entryPath));
      continue;
    }
    if (entry.isFile() && /(?<!\.test)\.(ts|tsx)$/.test(entry.name)) files.push(entryPath);
  }
  return files;
};

// importsSharedSettingsNormalizer 判断源码是否从共享契约层导入真正的系统设置秘密归一器。
const importsSharedSettingsNormalizer = sourceFile => sourceFile.statements.some(statement => {
  if (!ts.isImportDeclaration(statement) || !ts.isStringLiteral(statement.moduleSpecifier)) return false;
  if (!statement.moduleSpecifier.text.endsWith('/shared/api-contract/settings')) return false;
  // importClause 保存当前 import 的默认、命名或 namespace 导入定义。
  const importClause = statement.importClause;
  if (!importClause || !importClause.namedBindings || !ts.isNamedImports(importClause.namedBindings)) return false;
  return importClause.namedBindings.elements.some(element => element.name.text === 'normalizeSystemSettingsUpdate');
});

// isSystemSettingsWrite 判断调用是否直接写入系统设置端点；读取和其他端点不受此规则影响。
const isSystemSettingsWrite = node => {
  if (!ts.isCallExpression(node) || !ts.isPropertyAccessExpression(node.expression)) return false;
  if (node.expression.name.text !== 'PUT' || !ts.isIdentifier(node.expression.expression) || node.expression.expression.text !== 'contractClient') return false;
  // endpointArgument 是 contractClient.PUT 的首个路径参数。
  const endpointArgument = node.arguments[0];
  return ts.isStringLiteral(endpointArgument) && endpointArgument.text === systemSettingsPath;
};

// usesSharedSettingsNormalizer 判断写入选项中的 body 是否直接由共享秘密归一器生成，禁止表单对象和 as never 绕过类型。
const usesSharedSettingsNormalizer = node => {
  // requestOptions 是 PUT 调用的第二个参数，必须是包含 body 的对象字面量。
  const requestOptions = node.arguments[1];
  if (!requestOptions || !ts.isObjectLiteralExpression(requestOptions)) return false;
  // bodyProperty 是写入请求体字段；不存在即无法满足系统设置的显式秘密命令契约。
  const bodyProperty = requestOptions.properties.find(property => ts.isPropertyAssignment(property) && ts.isIdentifier(property.name) && property.name.text === 'body');
  if (!bodyProperty || !ts.isPropertyAssignment(bodyProperty) || !ts.isCallExpression(bodyProperty.initializer)) return false;
  // normalizerCall 是生成请求体的调用，只接受共享函数的直接调用，避免本地同名包装绕过门禁。
  const normalizerCall = bodyProperty.initializer;
  return ts.isIdentifier(normalizerCall.expression) && normalizerCall.expression.text === 'normalizeSystemSettingsUpdate';
};

// verifySystemSettingsWriteBoundary 强制所有 feature 的系统设置 PUT 经过共享秘密归一器，避免敏感字段落入普通 values 或顶层表单对象。
const verifySystemSettingsWriteBoundary = () => {
  // violations 保存每个不符合系统设置敏感命令边界的源码位置。
  const violations = [];
  for (const filePath of listFeatureSourceFiles(featureRoot)) {
    // sourceText 是待扫描的 TypeScript 源码文本。
    const sourceText = fs.readFileSync(filePath, 'utf8');
    // sourceFile 是用于可靠识别调用、对象字段和类型断言的 TypeScript AST。
    const sourceFile = ts.createSourceFile(filePath, sourceText, ts.ScriptTarget.Latest, true);
    // hasSystemSettingsWrite 标记当前文件是否直接写入系统设置端点，用于校验共享函数的实际导入。
    let hasSystemSettingsWrite = false;
    // visit 递归检查当前节点及其子节点中的系统设置写入调用。
    const visit = node => {
      if (isSystemSettingsWrite(node)) {
        hasSystemSettingsWrite = true;
        if (!usesSharedSettingsNormalizer(node)) {
          // lineNumber 是违规 PUT 调用在源码中的一基行号，供修复时精确定位。
          const lineNumber = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile)).line + 1;
          violations.push(`${path.relative(frontendRoot, filePath)}:${lineNumber} 必须使用 normalizeSystemSettingsUpdate 构造 /api/v1/settings/system 请求体`);
        }
      }
      ts.forEachChild(node, visit);
    };
    visit(sourceFile);
    if (hasSystemSettingsWrite && !importsSharedSettingsNormalizer(sourceFile)) {
      violations.push(`${path.relative(frontendRoot, filePath)} 必须从 shared/api-contract/settings 导入 normalizeSystemSettingsUpdate`);
    }
  }
  if (violations.length > 0) throw new Error(`系统设置敏感命令门禁失败：\n${violations.join('\n')}`);
};

try {
  execFileSync(process.execPath, [generator, path.join(repositoryRoot, 'api', 'openapi.yaml'), '-o', temporarySchema], {
    cwd: frontendRoot,
    stdio: 'inherit',
  });
  const generatedContent = fs.readFileSync(temporarySchema);
  const checkedInContent = fs.readFileSync(checkedInSchema);
  if (!generatedContent.equals(checkedInContent)) {
    throw new Error('生成的 OpenAPI TypeScript 类型与提交产物不一致；请运行 npm run api:generate --prefix frontend。');
  }
  verifySystemSettingsWriteBoundary();
} finally {
  fs.rmSync(temporaryDirectory, { recursive: true, force: true });
}
