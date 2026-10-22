// Validate the published native assets without invoking either provider.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const json = async (path) => JSON.parse(await readFile(join(root, path), 'utf8'));
const pkg = await json('package.json');
const lock = await json('npm-shrinkwrap.json');
assert.equal(lock.version, pkg.version);
assert.equal(lock.packages[''].version, pkg.version);
for (const provider of ['claude', 'codex']) {
  const base = `plugins/${provider}/asana-sessions`;
  const manifest = await json(`${base}/.${provider === 'claude' ? 'claude' : 'codex'}-plugin/plugin.json`);
  assert.equal(manifest.name, pkg.name);
  assert.equal(manifest.version, pkg.version);
  const hooks = (await json(`${base}/hooks/hooks.json`)).hooks;
  assert.deepEqual(Object.keys(hooks), ['UserPromptSubmit', 'Stop']);
  for (const groups of Object.values(hooks)) {
    assert.equal(groups.length, 1);
    assert.equal(groups[0].hooks.length, 1);
    const hook = groups[0].hooks[0];
    assert.equal(hook.type, 'command');
    assert.equal(hook.command, `asana-sessions hook ${provider}`);
    assert.equal(hook.timeout, 15);
  }
  const skill = await readFile(join(root, base, 'skills/link-asana-task/SKILL.md'), 'utf8');
  assert.ok(skill.includes(`--provider ${provider} --current`));
  assert.ok(skill.includes('--replace'));
  assert.ok(skill.includes('--session <id>'));
  const market = await json(provider === 'claude'
    ? 'plugins/claude/.claude-plugin/marketplace.json'
    : 'plugins/codex/.agents/plugins/marketplace.json');
  assert.equal(market.name, pkg.name);
  assert.equal(market.plugins.length, 1);
  assert.equal(market.plugins[0].name, pkg.name);
  assert.equal(provider === 'claude' ? market.plugins[0].source : market.plugins[0].source.path, './asana-sessions');
  if (provider === 'claude') assert.equal(market.plugins[0].version, pkg.version);
}
console.log('plugin assets ok');
