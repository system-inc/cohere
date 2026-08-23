// Node harness: same bundles, same options, one process, timing only the formatting.
const fs = require('fs');
const path = require('path');
const dist = '/Users/kirkouimet/Projects/system/prettier/dist/prettier';
const vm = require('vm');
const sandbox = { console, TextEncoder, TextDecoder, URL, process };
sandbox.globalThis = sandbox;
vm.createContext(sandbox);
for (const f of ['standalone.js','plugins/estree.js','plugins/typescript.js','plugins/babel.js','plugins/postcss.js','plugins/markdown.js','plugins/graphql.js','plugins/yaml.js']) {
  vm.runInContext(fs.readFileSync(path.join(dist,f),'utf8'), sandbox, {filename:f});
}
const list = fs.readFileSync(process.argv[2],'utf8').trim().split('\n').filter(Boolean);
const parserFor = (p) => {
  if (path.basename(p) === 'package.json') return 'json-stringify';
  const e = path.extname(p);
  return {'.ts':'typescript','.tsx':'typescript','.js':'babel','.jsx':'babel','.mjs':'babel','.cjs':'babel','.json':'json','.css':'css','.scss':'scss','.less':'less','.md':'markdown','.graphql':'graphql','.gql':'graphql','.yaml':'yaml','.yml':'yaml'}[e];
};
(async () => {
  let bytes = 0, n = 0;
  const t0 = process.hrtime.bigint();
  for (const f of list) {
    const parser = parserFor(f);
    if (!parser) continue;
    const src = fs.readFileSync(f,'utf8');
    await sandbox.prettier.format(src, {parser, plugins: sandbox.prettierPlugins, tabWidth:4, useTabs:false, semi:true, singleQuote:true, printWidth:120});
    bytes += src.length; n++;
  }
  const ms = Number(process.hrtime.bigint()-t0)/1e6;
  console.log(`node in-process: files=${n} bytes=${bytes} total=${ms.toFixed(0)}ms per-file=${(ms/n).toFixed(1)}ms ${(bytes/1024/(ms/1000)).toFixed(1)} KB/s`);
})();
