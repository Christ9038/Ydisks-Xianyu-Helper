// 从 docs/wiki（唯一内容源）生成 VitePress 页面：中文在根路径，英文在 /en/。
// 处理：去掉语言切换行与 Wiki 同步说明、清理标题后缀、把 Wiki 页内链接改写为站内路径、追加界面截图。
import { readFileSync, writeFileSync, mkdirSync, rmSync, cpSync, existsSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { groups } from './pages.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const wiki = resolve(root, '../docs/wiki');

// stemToSlug 把所有 Wiki 文件名（中英文，含/不含 .zh-CN）映射到站内 slug。
const stemToSlug = new Map();
for (const g of groups) for (const [slug, zh, en] of g.pages) {
  for (const stem of [zh, en, zh.replace(/\.zh-CN$/, '')]) stemToSlug.set(stem, slug);
}

/** rewriteLinks 把 ](Wiki页名#锚点) 改为当前语言的站内链接，未知页面名原样保留并让构建报死链。 */
function rewriteLinks(text, prefix) {
  return text.replace(/\]\(([^)#\s]+)(#[^)\s]*)?\)/g, (all, target, hash = '') => {
    if (/^(https?:|mailto:|\/)/.test(target)) return all;
    const slug = stemToSlug.get(decodeURIComponent(target));
    if (!slug) return all;
    return `](${prefix}${slug === 'index' ? '' : slug}${hash})`;
  });
}

/** cleanBody 去掉语言切换行、标题里的“（中文）”后缀，并移除仅对维护者有意义的 Wiki 同步章节。 */
function cleanBody(text) {
  const lines = text.split('\n');
  const out = lines.filter((l, i) => !(i < 5 && /^\[?(English|中文)\]?(\([^)]*\))?\s*\|\s*\[?(English|中文)/.test(l)));
  let body = out.join('\n');
  body = body.replace(/^(# .+?)\s*[（(](中文|English)[）)]\s*$/m, '$1');
  body = body.replace(/\n## (GitHub Wiki 同步|GitHub Wiki synchronization)[\s\S]*$/, '\n');
  return body;
}

rmSync(join(root, 'en'), { recursive: true, force: true });
mkdirSync(join(root, 'en'), { recursive: true });
mkdirSync(join(root, 'public'), { recursive: true });
const imgSrc = resolve(root, '../docs/img');
rmSync(join(root, 'public/img'), { recursive: true, force: true });
mkdirSync(join(root, 'public/img'), { recursive: true });
for (const f of ['preview_account.png', 'preview_im.png', 'preview_auto.png', 'slogan.png']) cpSync(join(imgSrc, f), join(root, 'public/img', f));
cpSync(resolve(root, '../docs/assets/favicon.svg'), join(root, 'public/favicon.svg'));

const shots = {
  zh: '\n## 界面预览\n\n![账号管理](/img/preview_account.png)\n\n![在线聊天](/img/preview_im.png)\n\n![自动化规则](/img/preview_auto.png)\n',
  en: '\n## Screenshots\n\n![Accounts](/img/preview_account.png)\n\n![Chat](/img/preview_im.png)\n\n![Automation](/img/preview_auto.png)\n',
};

let count = 0;
for (const g of groups) for (const [slug, zh, en] of g.pages) {
  for (const [lang, stem] of [['zh', zh], ['en', en]]) {
    const src = join(wiki, `${stem}.md`);
    if (!existsSync(src)) throw new Error(`缺少源文件 ${src}`);
    const prefix = lang === 'zh' ? '/' : '/en/';
    let body = rewriteLinks(cleanBody(readFileSync(src, 'utf8')), prefix);
    if (slug === 'index') body = body.trimEnd() + '\n' + shots[lang];
    const outFile = lang === 'zh' ? join(root, `${slug}.md`) : join(root, 'en', `${slug}.md`);
    // 页面含 {{order_id}} 等模板变量文本，需关闭 Vue 插值解析（::: v-pre 容器不影响标题目录）。
    body = body.replace(/^(# .+)$/m, '$1\n\n::: v-pre') + '\n:::\n';
    writeFileSync(outFile, `---\nsource: ${stem}.md\n---\n\n${body}`);
    count++;
  }
}
console.log(`已生成 ${count} 个页面`);
