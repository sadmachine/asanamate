// Public command dispatch. Exit codes: 0 success or complete lookup, 1 failure
// or partial/unavailable lookup, 2 usage error.
import { execFile } from 'node:child_process';
import { readFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { createInterface } from 'node:readline/promises';
import { fileURLToPath } from 'node:url';
import { parseArgs, promisify } from 'node:util';
import { asanamateList, asanamateOpen } from './asanamate.js';
import { asanamateAgents } from './agent-links.js';
import { display } from './display.js';
import { Cancelled, createProviders, link, lookup, open, unlink } from './engine.js';
import { ProviderError, UsageError } from './errors.js';
import { hook } from './hook.js';
import { parseTask } from './marker.js';
import { helperOnPath, pluginStatus, remove, runtimeStatus, setup } from './setup.js';

const pkg = JSON.parse(await readFile(join(dirname(fileURLToPath(import.meta.url)), '..', 'package.json'), 'utf8'));

export const VERSION = pkg.version;

const USAGE = `asana-sessions ${VERSION}: find and resume Claude Code and Codex conversations by Asana ticket

Usage:
  asana-sessions list <task-url-or-gid> [--json]
  asana-sessions open <task-url-or-gid> --provider <claude|codex> --session <id>
  asana-sessions link --provider <claude|codex> (--session <id> | --current) --task <task-url-or-gid> [--replace]
  asana-sessions unlink --provider <claude|codex> (--session <id> | --current)
  asana-sessions setup [--provider <claude|codex|both>]
  asana-sessions remove [--provider <claude|codex|both>]
  asana-sessions doctor
  asana-sessions help | version

  asana-sessions hook <claude|codex>   provider plugin hook; reads one event on stdin

  asana-sessions asanamate list   asanamate conversations.command adapter
  asana-sessions asanamate open   asanamate conversations.open adapter
  asana-sessions asanamate agents -- <agent-source> [args...]   optional ticket-link decorator
`;

// terminalIO asks on the controlling terminal. Prompts and notices go to stderr.
export function terminalIO(stdin = process.stdin, stderr = process.stderr) {
  return {
    interactive: Boolean(stdin.isTTY && stderr.isTTY),
    say: (line) => stderr.write(`${line}\n`),
    async ask(question) {
      const rl = createInterface({ input: stdin, output: stderr });
      try {
        return await rl.question(question);
      } finally {
        rl.close();
      }
    },
  };
}

export async function main(argv, ctx = {}) {
  const c = {
    stdout: process.stdout, stderr: process.stderr, stdin: process.stdin, env: process.env,
    ...ctx,
  };
  c.abort ??= new AbortController();
  c.makeProviders ??= (env) => createProviders({ env, version: VERSION, signal: c.abort.signal });
  c.providers ??= c.makeProviders(c.env);
  c.version ??= VERSION;
  c.io ??= terminalIO(c.stdin, c.stderr);
  try {
    return await dispatch(argv, c);
  } catch (err) {
    if (err instanceof Cancelled) {
      c.stderr.write('asana-sessions: cancelled\n');
      return 1;
    }
    c.stderr.write(`asana-sessions: ${display(err.message)}\n`);
    if (err instanceof UsageError) {
      c.stderr.write('Run `asana-sessions help` for usage.\n');
      return 2;
    }
    return 1;
  }
}

const OPTIONS = {
  json: { type: 'boolean' },
  provider: { type: 'string' },
  session: { type: 'string' },
  task: { type: 'string' },
  replace: { type: 'boolean' },
  current: { type: 'boolean' },
};

// parse requires every allowed string option except those in optional.
function parse(args, allowed, positionals = 0, optional = []) {
  let parsed;
  try {
    parsed = parseArgs({ args, options: Object.fromEntries(allowed.map((k) => [k, OPTIONS[k]])), allowPositionals: true, strict: true });
  } catch (err) {
    throw new UsageError(err.message);
  }
  if (parsed.positionals.length !== positionals) {
    throw new UsageError(`expected ${positionals} argument(s)`);
  }
  for (const name of allowed) {
    if (OPTIONS[name].type === 'string' && parsed.values[name] === undefined && !optional.includes(name)) {
      throw new UsageError(`--${name} is required`);
    }
  }
  return { ...parsed.values, args: parsed.positionals };
}

async function dispatch([command, ...rest], c) {
  const json = (value) => c.stdout.write(`${JSON.stringify(value, null, 2)}\n`);
  switch (command) {
    case undefined:
    case 'help':
    case '--help':
    case '-h':
      c.stdout.write(USAGE);
      return 0;
    case 'version':
    case '--version':
      c.stdout.write(`${VERSION}\n`);
      return 0;
    case 'list': {
      const o = parse(rest, ['json'], 1);
      const { status, ...result } = await lookup(c.providers, parseTask(o.args[0]));
      if (o.json) {
        json(result);
      } else {
        printList(result, c.stdout);
      }
      return status === 'complete' ? 0 : 1;
    }
    case 'open': {
      const o = parse(rest, ['provider', 'session'], 1);
      return open(c.providers, { provider: o.provider, sessionId: o.session, gid: parseTask(o.args[0]) }, { ...c.io, env: c.env });
    }
    case 'link': {
      const o = parse(rest, ['provider', 'session', 'current', 'task', 'replace'], 0, ['session']);
      json(await link(c.providers, { provider: o.provider, sessionId: session(o, c), gid: parseTask(o.task), replace: Boolean(o.replace) }, { env: c.env }));
      return 0;
    }
    case 'unlink': {
      const o = parse(rest, ['provider', 'session', 'current'], 0, ['session']);
      json(await unlink(c.providers, { provider: o.provider, sessionId: session(o, c) }, { env: c.env }));
      return 0;
    }
    case 'hook':
      if (rest.length !== 1) {
        throw new UsageError('expected `hook claude` or `hook codex`');
      }
      return hook(rest[0], c);
    case 'setup':
      return setup(c, parse(rest, ['provider'], 0, ['provider']));
    case 'remove':
      return remove(c, parse(rest, ['provider'], 0, ['provider']));
    case 'doctor':
      parse(rest, []);
      return doctor(c);
    case 'asanamate':
      if (rest[0] === 'agents') {
        return asanamateAgents(c, rest.slice(1));
      }
      if (rest.length === 1 && rest[0] === 'list') {
        return asanamateList(c);
      }
      if (rest.length === 1 && rest[0] === 'open') {
        return asanamateOpen(c);
      }
      throw new UsageError('expected `asanamate list`, `asanamate open`, or `asanamate agents -- <agent-source>`');
    default:
      throw new UsageError(`unknown command ${JSON.stringify(display(command))}`);
  }
}

// session is --session, or with --current the ID the provider exports to the
// commands it runs. Without that variable there is no reliable current ID.
function session(o, c) {
  if (Boolean(o.current) === (o.session !== undefined)) {
    throw new UsageError('pass exactly one of --session <id> or --current');
  }
  if (o.session !== undefined) {
    return o.session;
  }
  const name = c.providers[o.provider]?.currentEnv;
  const id = name ? c.env[name] : undefined;
  if (!id) {
    throw new UsageError(`no current ${display(o.provider)} session ID is available here${name ? ` (${name} is not set)` : ''}; pass --session <id>`);
  }
  return id;
}

function printList({ conversations, diagnostics }, out) {
  if (!conversations.length) {
    out.write('No linked conversations.\n');
  }
  for (const r of conversations) {
    const when = r.updated_at ? r.updated_at.replace('T', ' ').slice(0, 16) : 'unknown time';
    out.write(`${display(r.provider)}  ${display(r.session_id)}  ${when}${r.archived ? '  archived' : ''}  activity: ${r.activity}\n`);
    out.write(`  ${display(r.title)}\n`);
    if (r.cwd || r.branch) {
      out.write(`  ${[r.cwd && `cwd: ${display(r.cwd)}`, r.branch && `branch: ${display(r.branch)}`].filter(Boolean).join('  ')}\n`);
    }
  }
  for (const d of diagnostics) {
    out.write(`${d.severity}: ${d.provider}: ${d.code}: ${d.message}\n`);
  }
}

// doctor checks runtimes, executables, histories, and metadata APIs without
// starting a model turn. Missing providers are reported, not failures.
async function doctor(c) {
  const run = promisify(execFile);
  const runtime = await runtimeStatus(c);
  const lines = runtime.lines;
  let failed = !runtime.ok;
  for (const name of ['claude', 'codex']) {
    try {
      const { stdout } = await run(name, ['--version'], { env: c.env, timeout: 5000 });
      lines.push(`ok: ${name} ${display(stdout)}`);
    } catch (err) {
      lines.push(err.code === 'ENOENT' ? `missing: ${name} is not on PATH` : `error: ${name} --version failed`);
      failed ||= err.code !== 'ENOENT';
    }
    try {
      await c.providers[name].probe();
      lines.push(`ok: ${name} history metadata is readable`);
    } catch (err) {
      const e = err instanceof ProviderError ? err : new ProviderError('provider_error', String(err.message));
      lines.push(`${e.code === 'unavailable' ? 'missing' : 'error'}: ${name}: ${display(e.message)}`);
      failed ||= e.code !== 'unavailable';
    }
    const s = await pluginStatus(name, c);
    lines.push(`${s.state === 'error' ? 'error' : 'info'}: ${name} plugin: ${s.state}: ${s.detail}`);
    failed ||= s.state === 'error';
  }
  lines.push((await helperOnPath(c)).line);
  c.stdout.write(`${lines.join('\n')}\n`);
  return failed ? 1 : 0;
}
