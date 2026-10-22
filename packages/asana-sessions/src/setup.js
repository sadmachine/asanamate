// Provider plugin registration through each provider's native plugin
// commands. setup and remove change only this package's own marketplace and
// plugin; pluginStatus never changes anything.
import { execFile } from 'node:child_process';
import { access, constants, readFile, realpath } from 'node:fs/promises';
import { delimiter, dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { display } from './display.js';
import { UsageError } from './errors.js';
import { sweepPending } from './pending.js';

export const PLUGIN_ID = 'asana-sessions@asana-sessions';
const MARKETPLACE = 'asana-sessions';
const PKG = join(dirname(fileURLToPath(import.meta.url)), '..');

// TESTED holds the oldest provider versions the plugins were validated with.
export const TESTED = { claude: '2.1.301', codex: '0.162.1' };

export const AUTO_LINK_RULE = 'Automatic linking: when a prompt you submit contains exactly one Asana task URL, '
  + 'the hook links that conversation to the task, even if you only quoted the link. It never replaces an existing link. '
  + 'For deliberate linking only, skip the plugins and run `asana-sessions link` yourself.';

const NATIVE = {
  claude: {
    label: 'Claude Code',
    register: (root) => [['plugin', 'marketplace', 'add', root], ['plugin', 'install', PLUGIN_ID]],
    unregister: [['plugin', 'uninstall', PLUGIN_ID], ['plugin', 'marketplace', 'remove', MARKETPLACE]],
    next: 'Start a new Claude Code session, or run /reload-plugins in an open one, to load the hooks and skill.',
  },
  codex: {
    label: 'Codex',
    register: (root) => [['plugin', 'marketplace', 'add', root], ['plugin', 'add', PLUGIN_ID]],
    unregister: [['plugin', 'remove', PLUGIN_ID], ['plugin', 'marketplace', 'remove', MARKETPLACE]],
    next: 'Start Codex, run /hooks, then review and trust the asana-sessions prompt and Stop hooks; Codex skips untrusted plugin hooks.',
  },
};
const NAMES = Object.keys(NATIVE);

export const marketplaceRoot = (name, pkg = PKG) => join(pkg, 'plugins', name);
const pluginRoot = (name, pkg) => join(marketplaceRoot(name, pkg), 'asana-sessions');

export function execNative(command, args, env) {
  return new Promise((resolve) => {
    execFile(command, args, { env, timeout: 60000, maxBuffer: 16 * 1024 * 1024 }, (err, stdout, stderr) => {
      resolve({ ok: !err, missing: err?.code === 'ENOENT', stdout: String(stdout ?? ''), stderr: String(stderr ?? '') });
    });
  });
}

const firstLine = (r) => display((r.stderr.trim() || r.stdout.trim() || 'failed').split('\n')[0]).slice(0, 300);

function semver(text) {
  const m = String(text).match(/(\d+)\.(\d+)\.(\d+)/);
  return m ? m.slice(1).map(Number) : null;
}

function older(a, b) {
  for (let i = 0; i < 3; i++) {
    if (a[i] !== b[i]) {
      return a[i] < b[i];
    }
  }
  return false;
}

const same = async (a, b) => Boolean(a && b) && (await realpath(a).catch(() => a)) === (await realpath(b).catch(() => b));

function json(r) {
  try {
    return JSON.parse(r.stdout);
  } catch {
    return undefined;
  }
}

// pluginStatus reports one provider's plugin as missing (no CLI),
// incompatible, not registered, stale, disabled, untrusted, ready, or error.
export async function pluginStatus(name, c) {
  const exec = c.exec ?? execNative;
  const version = await exec(name, ['--version'], c.env);
  if (version.missing) {
    return { state: 'missing', detail: `${name} is not on PATH` };
  }
  const v = semver(version.stdout);
  if (!version.ok || !v) {
    return { state: 'error', detail: `${name} --version failed` };
  }
  if (older(v, semver(TESTED[name]))) {
    return { state: 'incompatible', detail: `${name} ${v.join('.')} is older than the tested ${TESTED[name]}` };
  }
  const listed = await exec(name, ['plugin', 'list', '--json'], c.env);
  const data = json(listed);
  if (!listed.ok || data === undefined) {
    return { state: 'error', detail: `${name} plugin list failed: ${firstLine(listed)}` };
  }
  const entries = name === 'claude' ? data : data?.installed;
  const entry = Array.isArray(entries) ? entries.find((e) => (e?.id ?? e?.pluginId) === PLUGIN_ID) : undefined;
  if (!entry) {
    return { state: 'not registered', detail: 'run `asana-sessions setup`' };
  }
  const source = name === 'claude' ? entry.readFromFolder : entry.source?.path;
  if (entry.version !== c.version || !(await same(source, pluginRoot(name, c.pkg ?? PKG)))) {
    return { state: 'stale', detail: `registered ${display(entry.version)} from ${display(source ?? 'unknown')}; rerun \`asana-sessions setup\`` };
  }
  if (entry.enabled === false) {
    return { state: 'disabled', detail: `enable ${PLUGIN_ID} in ${NATIVE[name].label}` };
  }
  return name === 'codex' ? codexHooks(c) : { state: 'ready', detail: 'hooks load in new sessions' };
}

// codexHooks checks that Codex sees exactly one prompt and one Stop hook from
// the plugin, and whether the user has trusted their current definitions.
async function codexHooks(c) {
  let hooks;
  try {
    hooks = (await c.providers.codex.hooks(c.cwd ?? process.cwd())).filter((h) => h?.pluginId === PLUGIN_ID);
  } catch (err) {
    return { state: 'error', detail: `Codex hook list failed: ${display(err.message)}` };
  }
  const count = (event) => hooks.filter((h) => h.eventName === event).length;
  if (count('userPromptSubmit') !== 1 || count('stop') !== 1) {
    return { state: 'stale', detail: `Codex loads ${count('userPromptSubmit')} prompt and ${count('stop')} Stop hooks from the plugin; rerun \`asana-sessions setup\`` };
  }
  if (hooks.some((h) => h.enabled === false)) {
    return { state: 'disabled', detail: 'enable the asana-sessions hooks in Codex /hooks' };
  }
  if (hooks.some((h) => h.trustStatus !== 'trusted')) {
    return { state: 'untrusted', detail: 'run /hooks in Codex, then review and trust the asana-sessions prompt and Stop hooks' };
  }
  return { state: 'ready', detail: 'hooks are trusted' };
}

// helperOnPath finds the asana-sessions executable a provider's hooks would
// run with this PATH, and whether it is this installation.
export async function helperOnPath(c) {
  for (const dir of String(c.env.PATH ?? '').split(delimiter).filter(Boolean)) {
    const path = join(dir, 'asana-sessions');
    if (await access(path, constants.X_OK).then(() => true, () => false)) {
      const self = join(c.pkg ?? PKG, 'bin', 'asana-sessions.js');
      return (await same(path, self))
        ? { ok: true, line: `ok: asana-sessions on PATH is ${display(path)}` }
        : { ok: false, line: `warning: asana-sessions on PATH is another installation, ${display(path)}; hooks will run that one` };
    }
  }
  return { ok: false, line: 'warning: asana-sessions is not on PATH; hooks cannot run it. Install it globally or fix the PATH providers start with.' };
}

async function select(c, provider, verb) {
  if (provider !== undefined) {
    if (provider === 'both') {
      return NAMES;
    }
    if (!NAMES.includes(provider)) {
      throw new UsageError('--provider must be claude, codex, or both');
    }
    return [provider];
  }
  if (!c.io.interactive) {
    throw new UsageError(`pass --provider claude, codex, or both to ${verb} without a terminal`);
  }
  const answer = (await c.io.ask(`${verb[0].toUpperCase()}${verb.slice(1)} plugins for claude, codex, or both? (empty to cancel) `)).trim().toLowerCase();
  if (!answer) {
    return [];
  }
  return select({ ...c, io: { interactive: false } }, answer, verb);
}

async function run(c, name, commands, out) {
  const exec = c.exec ?? execNative;
  for (const args of commands) {
    const r = await exec(name, args, c.env);
    if (!r.ok) {
      out.push(`error: ${name} ${args.join(' ')}: ${firstLine(r)}`);
      return false;
    }
  }
  return true;
}

// Shared runtime checks keep setup and doctor on the same compatibility floor.
export async function runtimeStatus(c) {
  const lines = [];
  let ok = Number(process.versions.node.split('.')[0]) >= 22;
  lines.push(`${ok ? 'ok' : 'error'}: node ${process.versions.node}${ok ? '' : '; Node 22 or newer is required'}`);
  try {
    const installed = JSON.parse(await readFile(join(dirname(fileURLToPath(import.meta.resolve('@anthropic-ai/claude-agent-sdk'))), 'package.json'), 'utf8'));
    const required = JSON.parse(await readFile(join(c.pkg ?? PKG, 'package.json'), 'utf8')).dependencies['@anthropic-ai/claude-agent-sdk'];
    if (installed.version !== required) {
      throw new Error('version mismatch');
    }
    lines.push(`ok: @anthropic-ai/claude-agent-sdk ${installed.version}`);
  } catch {
    ok = false;
    lines.push('error: locked Claude Agent SDK is missing or mismatched; reinstall asana-sessions');
  }
  return { ok, lines };
}

export async function setup(c, { provider }) {
  const say = (line) => c.stdout.write(`${line}\n`);
  const helper = await helperOnPath(c);
  say(helper.line);
  const runtime = await runtimeStatus(c);
  runtime.lines.forEach(say);
  if (!runtime.ok) {
    return 1;
  }
  const before = {};
  for (const name of NAMES) {
    before[name] = await pluginStatus(name, c);
    say(`${name}: ${before[name].state}: ${before[name].detail}`);
  }
  say('');
  say(AUTO_LINK_RULE);
  const chosen = await select(c, provider, 'install');
  if (!chosen.length) {
    say('Cancelled; nothing was registered.');
    return 1;
  }
  let failed = !helper.ok;
  for (const name of chosen) {
    if (['missing', 'incompatible'].includes(before[name].state)) {
      say(`skipped ${name}: ${before[name].detail}`);
      failed = true;
      continue;
    }
    const out = [];
    // Native install can keep an old cached version or marketplace path.
    // Replace only our stale registration; native trust still checks its hash.
    if (before[name].state === 'stale' && !(await run(c, name, NATIVE[name].unregister, out))) {
      out.forEach(say);
      failed = true;
      continue;
    }
    if (await run(c, name, NATIVE[name].register(marketplaceRoot(name, c.pkg ?? PKG)), out)) {
      say(`registered ${PLUGIN_ID} with ${NATIVE[name].label}`);
      say(`next: ${NATIVE[name].next}`);
    } else {
      out.forEach(say);
      failed = true;
    }
  }
  say('');
  for (const name of chosen) {
    const s = await pluginStatus(name, c);
    say(`${name}: ${s.state}: ${s.detail}`);
    failed ||= s.state !== 'ready';
  }
  return failed ? 1 : 0;
}

export async function remove(c, { provider }) {
  const say = (line) => c.stdout.write(`${line}\n`);
  const chosen = await select(c, provider, 'remove');
  if (!chosen.length) {
    say('Cancelled; nothing was removed.');
    return 1;
  }
  let failed = false;
  for (const name of chosen) {
    await sweepPending(c.env, { provider: name });
    const s = await pluginStatus(name, c);
    if (s.state === 'missing') {
      say(`${name}: ${s.detail}; nothing to remove`);
      continue;
    }
    const out = [];
    const registered = s.state !== 'not registered';
    const [uninstall, market] = NATIVE[name].unregister;
    if (registered && !(await run(c, name, [uninstall], out))) {
      out.forEach(say);
      failed = true;
      continue;
    }
    // The marketplace may remain from an interrupted setup; it is ours by name.
    const marketRemoved = await run(c, name, [market], out);
    if (registered && !marketRemoved) {
      out.forEach(say);
      failed = true;
    }
    say(registered || marketRemoved ? `removed ${PLUGIN_ID} from ${NATIVE[name].label}` : `${name}: not registered`);
  }
  say('Saved conversation titles and history are unchanged.');
  return failed ? 1 : 0;
}
