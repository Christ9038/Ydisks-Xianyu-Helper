// 校验生成类型未被人工修改，并把临时生成结果与仓库产物逐字节比较。
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const frontendRoot = path.resolve(import.meta.dirname, '..');
const repositoryRoot = path.resolve(frontendRoot, '..');
const temporaryDirectory = fs.mkdtempSync(path.join(os.tmpdir(), 'ydisks-openapi-'));
const temporarySchema = path.join(temporaryDirectory, 'schema.ts');
const checkedInSchema = path.join(frontendRoot, 'shared', 'api-contract', 'generated', 'schema.ts');
const generator = path.join(frontendRoot, 'node_modules', 'openapi-typescript', 'bin', 'cli.js');

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
} finally {
  fs.rmSync(temporaryDirectory, { recursive: true, force: true });
}
