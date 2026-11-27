// Node comparison harness: the same bundles, the same options, one process, timing only the
// formatting so the only variable left is the engine.
//
// Two things here were learned the hard way and are the reason this file has a comment at all.
//
// It uses require rather than a vm sandbox. An earlier version evaluated the bundles into a
// vm.createContext sandbox, which is the natural way to mirror what goja does, and it measured node
// at 562 KB/s where require measures 872 KB/s cold and 2,064 KB/s warm. V8 optimizes sandboxed code
// far less aggressively, so a sandboxed harness understates node by 2 to 3x and produced a ratio
// that could not be reproduced by anyone timing it the normal way.
//
// It runs three passes rather than one. V8 warms up substantially on the same input: 872, 1,747,
// 2,064 KB/s across three passes of the same twelve files. Any single-pass node number is a cold
// number. goja, measured the same way, is flat at 61 KB/s from twelve files to thirty-six, because
// it has no JIT at all. That asymmetry is the real finding: the gap is not one ratio, it is a cold
// gap of roughly 14x and a steady-state gap of roughly 34x.
//
// Nothing in the repository runs it; it is run by hand, with the Prettier fork named by the same
// variable the Go formatter reads in disk mode (ForkPathVariable in formatter.go), so both sides
// time the same bundles:
//
//   COHERE_PRETTIER_FORK=<fork checkout> node internal/format/prettier/testdata/node_inprocess.js <file list>
//
// The file list holds one path per line.
const fs = require('fs');
const path = require('path');

const forkPath = (process.env.COHERE_PRETTIER_FORK || '').trim();
if (!forkPath || !process.argv[2]) {
  process.stderr.write(
    'usage: COHERE_PRETTIER_FORK=<prettier fork checkout> node node_inprocess.js <file list>\n' +
      'COHERE_PRETTIER_FORK names the checkout whose dist/prettier holds the built bundles, as the Go formatter reads it.\n',
  );
  process.exit(2);
}
const bundlePath = path.resolve(forkPath, 'dist', 'prettier', 'index.cjs');
if (!fs.existsSync(bundlePath)) {
  process.stderr.write(`node_inprocess.js: no built Prettier bundle at ${bundlePath}; build the fork first\n`);
  process.exit(2);
}
const prettier = require(bundlePath);

const list = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').filter(Boolean);
const parserFor = (p) => {
  if (path.basename(p) === 'package.json') return 'json-stringify';
  return {'.ts':'typescript','.tsx':'typescript','.js':'babel','.jsx':'babel','.mjs':'babel','.cjs':'babel','.json':'json','.css':'css','.scss':'scss','.less':'less','.md':'markdown','.graphql':'graphql','.gql':'graphql','.yaml':'yaml','.yml':'yaml'}[path.extname(p)];
};

(async () => {
  for (let pass = 0; pass < 3; pass++) {
    let bytes = 0, n = 0;
    const start = process.hrtime.bigint();
    for (const file of list) {
      const parser = parserFor(file);
      if (!parser) continue;
      const source = fs.readFileSync(file, 'utf8');
      await prettier.format(source, {parser, tabWidth: 4, useTabs: false, semi: true, singleQuote: true, printWidth: 120});
      bytes += source.length; n++;
    }
    const ms = Number(process.hrtime.bigint() - start) / 1e6;
    console.log(`pass ${pass}: files=${n} bytes=${bytes} ${ms.toFixed(0)}ms ${(bytes/1024/(ms/1000)).toFixed(0)} KB/s`);
  }
})();
