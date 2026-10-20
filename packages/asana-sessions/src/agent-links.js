// Optional agent-source decorator. Core receives task identity, not provider
// APIs or title conventions. Source failures fail the refresh; metadata failures
// preserve the source's repo/branch fallback.
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { display } from './display.js';
import { UsageError } from './errors.js';
import { ticketLink, validGID } from './marker.js';

const run = promisify(execFile);

export async function asanamateAgents(c, args) {
  if (args[0] !== '--' || !args[1]) {
    throw new UsageError('expected `asanamate agents -- <agent-source> [args...]`');
  }
  const source = run(args[1], args.slice(2), {
    env: c.env, timeout: 2000, killSignal: 'SIGKILL', maxBuffer: 4 * 1024 * 1024,
    signal: c.abort.signal,
  });
  source.child.stdin.end(); // Agent sources receive EOF, never a waiting pipe.
  const { stdout, stderr } = await source;
  if (stderr) c.stderr.write(stderr);
  const rows = stdout.split('\n').filter((line) => line.includes('\t') && line.split('\t')[0].trim())
    .map((line) => line.replace(/\r$/, '').split('\t'));
  const needed = new Set();
  for (const row of rows) {
    const name = row[7]?.trim();
    const id = row[8]?.trim();
    if (!row[6]?.trim() && Object.hasOwn(c.providers, name) && c.providers[name].validID(id ?? '')) {
      needed.add(name);
    }
  }
  const histories = new Map(await Promise.all([...needed].map(async (name) => {
    let timer;
    try {
      const timeout = new Promise((_, reject) => {
        timer = setTimeout(() => {
          reject(new Error('conversation lookup timed out'));
          c.abort.abort();
        }, 6000);
      });
      const sessions = await Promise.race([c.providers[name].list(), timeout]);
      return [name, new Map(sessions.map((s) => [s.sessionId, ticketLink(s.name, s.tag)]))];
    } catch (err) {
      c.stderr.write(`asana-sessions: ${name}: ${display(err.message)}; using repo/branch fallback\n`);
      return [name, new Map()];
    } finally {
      clearTimeout(timer);
    }
  })));
  for (const row of rows) {
    if (!row[6]?.trim()) {
      const link = histories.get(row[7]?.trim())?.get(row[8]?.trim());
      if (link?.conflict.length) {
        row[6] = '-'; // Conflict must not become a branch-based association.
        c.stderr.write(`asana-sessions: ${display(row[7])} session ${display(row[8])}: conflicting ticket links; repair with link --replace\n`);
      } else if (validGID(link?.gid)) {
        row[6] = link.gid;
      }
    }
    c.stdout.write(`${row.join('\t')}\n`);
  }
  return 0;
}
