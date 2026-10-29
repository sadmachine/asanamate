// The lookup, link, and open engine shared by the public commands and the
// asanamate adapter. It takes plain validated values and knows no host program.
import { spawn as nodeSpawn } from 'node:child_process';
import { stat } from 'node:fs/promises';
import { constants, homedir } from 'node:os';
import { resolve } from 'node:path';
import { createClaude } from './claude.js';
import { createCodex } from './codex.js';
import { display } from './display.js';
import { ProviderError, UsageError, providerError } from './errors.js';
import { withLock } from './lock.js';
import { DEFAULT_PREFIX, linkTitle, splitTitle, tagGID, ticketLink, validGID } from './marker.js';
import { peekPending, savePending, takePending } from './pending.js';

// PROTOCOL_VERSION versions the JSON record shape, independently of package SemVer.
export const PROTOCOL_VERSION = 1;

export function createProviders(options = {}) {
  return { claude: createClaude(options), codex: createCodex(options) };
}

// Cancelled is a user's choice not to proceed; it is not a provider failure.
export class Cancelled extends Error {}

function pick(providers, name, sessionId) {
  const p = Object.hasOwn(providers, name) ? providers[name] : null;
  if (!p) {
    throw new UsageError(`unsupported provider ${JSON.stringify(display(name))}; use claude or codex`);
  }
  if (sessionId !== undefined && !p.validID(sessionId)) {
    throw new UsageError(`not a ${name} session ID: ${JSON.stringify(display(sessionId))}`);
  }
  return p;
}

function severity(code) {
  return code === 'unavailable' ? 'info' : code === 'incomplete' || code === 'metadata_conflict' ? 'warning' : 'error';
}

// lookup finds every session linked to gid. status is complete, partial (a
// warning or error diagnostic), or unavailable (no provider history at all).
export async function lookup(providers, gid) {
  if (!validGID(gid)) {
    throw new UsageError('invalid task GID');
  }
  const outcomes = await Promise.all(Object.values(providers).map(async (p) => {
    try {
      return { p, sessions: await deadline(p.list(gid), LIST_TIMEOUT_MS, p.name) };
    } catch (err) {
      return { p, err: providerError(err, p.name) };
    }
  }));
  const conversations = [];
  const diagnostics = [];
  for (const { p, sessions, err } of outcomes) {
    if (err) {
      diagnostics.push({ provider: p.name, code: err.code, severity: severity(err.code), message: display(err.message) });
      continue;
    }
    for (const s of sessions) {
      const link = ticketLink(s.name, s.tag);
      if (link.gid === gid) {
        conversations.push({ ...record(s, gid), title: await shownTitle(p, s, gid) });
      } else if (link.conflict.includes(gid)) {
        diagnostics.push({
          provider: p.name, code: 'metadata_conflict', severity: 'warning',
          message: `session ${s.sessionId} links tickets ${link.conflict.join(', ')}; repair it with link --replace`,
        });
      }
    }
  }
  conversations.sort((a, b) => (b.updated_at ?? '').localeCompare(a.updated_at ?? ''));
  const available = outcomes.some((o) => o.err?.code !== 'unavailable');
  const status = !available ? 'unavailable' : diagnostics.every((d) => d.severity === 'info') ? 'complete' : 'partial';
  return { version: PROTOCOL_VERSION, conversations, diagnostics, status };
}

// LIST_TIMEOUT_MS keeps a whole lookup inside the 10-second deadline of
// asanamate's conversation protocol.
const LIST_TIMEOUT_MS = 8000;

function deadline(promise, ms, name) {
  let timer;
  const late = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new ProviderError('incomplete', `${name} lookup timed out`)), ms);
  });
  return Promise.race([promise, late]).finally(() => clearTimeout(timer));
}

function record(s, gid) {
  return {
    provider: s.provider,
    session_id: s.sessionId,
    task_gid: gid,
    title: s.title,
    ...(s.cwd ? { cwd: s.cwd } : {}),
    ...(s.branch ? { branch: s.branch } : {}),
    ...(s.updatedAt ? { updated_at: s.updatedAt } : {}),
    archived: s.archived,
    activity: s.activity,
  };
}

// generated returns the provider's generated title for a session whose saved
// title uses the fallback prefix, or null. It never fails a lookup or link.
async function generated(p, s) {
  if (!p.generatedTitle || splitTitle(s?.name).prefix !== DEFAULT_PREFIX) {
    return null;
  }
  return p.generatedTitle(s.sessionId).catch(() => null);
}

// shownTitle lists a fallback-titled link by its generated title, with the
// same marker; the native title is unchanged.
async function shownTitle(p, s, gid) {
  const title = await generated(p, s);
  return title ? linkTitle(title, gid) : s.title;
}

async function mustRead(p, sessionId) {
  const s = await p.read(sessionId);
  if (!s) {
    throw new ProviderError('provider_error', `${p.name} session ${sessionId} was not found`);
  }
  return s;
}

// link writes the canonical marker for gid, derived from the latest read, then
// verifies the saved title. Another ticket's marker or recognized tag is a
// conflict unless replace is set; replace clears only a recognized Asana tag.
export async function link(providers, { provider, sessionId, gid, replace = false }, lockOptions) {
  const p = pick(providers, provider, sessionId);
  if (!validGID(gid)) {
    throw new UsageError('invalid task GID');
  }
  return withLock(provider, sessionId, async () => {
    const result = await linkSaved(p, sessionId, gid, replace, lockOptions);
    await takePending(lockOptions?.env, provider, sessionId);
    return result;
  }, lockOptions);
}

// Prompt deferral, Stop completion, and explicit mutations share the session
// lock. A pending GID cannot escape that lock and be applied after unlink.
export async function linkFromPrompt(providers, { provider, sessionId, gid }, lockOptions) {
  const p = pick(providers, provider, sessionId);
  if (!validGID(gid)) {
    throw new UsageError('invalid task GID');
  }
  return withLock(provider, sessionId, async () => {
    const saved = await readIfPersisted(p, sessionId);
    lockOptions?.signal?.throwIfAborted();
    // Claude may not have persisted yet. Codex must stay unnamed until its
    // first turn can generate a title; setting a name prevents generation.
    if ((!saved && provider === 'claude') || (provider === 'codex' && saved && !saved.name)) {
      const other = await savePending(lockOptions?.env, provider, sessionId, gid);
      return other ? { pendingTask: other } : null;
    }
    return linkSaved(p, sessionId, gid, false, lockOptions);
  }, lockOptions);
}

// completePending also retitles a link saved before the provider generated a
// title, at the first Stop that finds one. With whenNamed, a session the
// provider has not named yet keeps its pending link for a later call.
export async function completePending(providers, { provider, sessionId, whenNamed = false }, lockOptions) {
  const p = pick(providers, provider, sessionId);
  return withLock(provider, sessionId, async () => {
    if (whenNamed && !(await peekPending(lockOptions?.env, provider, sessionId) && (await readIfPersisted(p, sessionId))?.name)) {
      return null;
    }
    const gid = await takePending(lockOptions?.env, provider, sessionId);
    const result = gid ? await linkSaved(p, sessionId, gid, false, lockOptions) : null;
    await retitle(p, sessionId, lockOptions);
    return result;
  }, lockOptions);
}

// retitle replaces the fallback prefix of a single-ticket title with the
// provider's generated title, once one exists. Call only while holding the
// session's metadata lock. Failure keeps the fallback for a later Stop.
async function retitle(p, sessionId, lockOptions) {
  if (!p.generatedTitle) {
    return;
  }
  const s = await readIfPersisted(p, sessionId).catch(() => null);
  const { gids } = splitTitle(s?.name);
  const title = gids.length === 1 && await generated(p, s);
  if (!title) {
    return;
  }
  lockOptions?.signal?.throwIfAborted();
  await p.rename(sessionId, linkTitle(title, gids[0])).catch(() => {});
}

// Claude's first prompt can precede even the projects directory.
async function readIfPersisted(p, sessionId) {
  try {
    return await p.read(sessionId);
  } catch (err) {
    if (p.name === 'claude' && err instanceof ProviderError && err.code === 'unavailable'
      && err.message === 'Claude Code is not installed or has no history') {
      return null;
    }
    throw err;
  }
}

// Call only while holding this provider/session's metadata lock.
async function linkSaved(p, sessionId, gid, replace, lockOptions) {
  const provider = p.name;
  const s = await mustRead(p, sessionId);
  lockOptions?.signal?.throwIfAborted();
  const { prefix, gids } = splitTitle(s.name);
  const tagged = tagGID(s.tag);
  const others = [...new Set([...gids, tagged].filter((g) => g && g !== gid))];
  if (others.length && !replace) {
    throw new ProviderError('metadata_conflict', `session ${sessionId} already links ticket ${others.join(', ')}; pass --replace to relink it`);
  }
  const title = linkTitle(prefix || s.summary, gid);
  const clearTag = Boolean(tagged) && tagged !== gid;
  if (title === s.name && !clearTag) {
    return { provider, session_id: sessionId, task_gid: gid, title, changed: false };
  }
  if (title !== s.name) {
    await p.rename(sessionId, title);
  }
  if (clearTag) {
    lockOptions?.signal?.throwIfAborted();
    await p.setTag(sessionId, null);
  }
  const after = await mustRead(p, sessionId);
  if (after.name !== title || ticketLink(after.name, after.tag).gid !== gid) {
    throw new ProviderError('provider_error', `the saved ${provider} title did not verify; it may have been changed concurrently`);
  }
  return { provider, session_id: sessionId, task_gid: gid, title: after.name, changed: true };
}

// unlink removes trailing markers and a recognized Asana tag, keeping the
// human-readable title and any unrelated tag. It also cancels deferred links,
// including a first-prompt link whose native conversation is not saved yet.
export async function unlink(providers, { provider, sessionId }, lockOptions) {
  const p = pick(providers, provider, sessionId);
  return withLock(provider, sessionId, async () => {
    const pending = await takePending(lockOptions?.env, provider, sessionId);
    const s = pending ? await readIfPersisted(p, sessionId) : await mustRead(p, sessionId);
    lockOptions?.signal?.throwIfAborted();
    if (!s) {
      return { provider, session_id: sessionId, title: '', changed: true };
    }
    const { prefix, gids } = splitTitle(s.name);
    const tagged = tagGID(s.tag);
    if (!gids.length && !tagged) {
      return { provider, session_id: sessionId, title: s.name ?? '', changed: Boolean(pending) };
    }
    const title = gids.length ? prefix || DEFAULT_PREFIX : s.name;
    if (gids.length) {
      await p.rename(sessionId, title);
    }
    if (tagged) {
      await p.setTag(sessionId, null);
    }
    const after = await mustRead(p, sessionId);
    const left = ticketLink(after.name, after.tag);
    if (after.name !== title || left.gid || left.conflict.length) {
      throw new ProviderError('provider_error', `the saved ${provider} title did not verify; it may have been changed concurrently`);
    }
    return { provider, session_id: sessionId, title, changed: true };
  }, lockOptions);
}

// open revalidates one exact linked session against native history, settles
// activity and cwd with the user, then resumes it on the inherited terminal.
// It resolves to the provider's exit status.
export async function open(providers, { provider, sessionId, gid }, io) {
  const p = pick(providers, provider, sessionId);
  const found = await lookup({ [provider]: p }, gid);
  const rec = found.conversations.find((r) => r.session_id === sessionId);
  if (!rec) {
    const why = found.diagnostics.map((d) => d.message).join('; ');
    throw new ProviderError('provider_error', why || `${provider} session ${sessionId} no longer exists or no longer links ticket ${gid}`);
  }
  if (rec.branch) {
    io.say(`Recorded branch: ${display(rec.branch)} (resuming does not restore Git state)`);
  }
  const target = p.resume(sessionId);
  const manual = [target.command, ...target.args].join(' ');
  if (rec.activity === 'active') {
    throw new ProviderError('provider_error', `this conversation is active elsewhere; use the program that owns it, or stop it and retry`);
  }
  // Disclose before any question: a full-screen provider hides later output.
  if (rec.archived) {
    io.say(`This conversation is archived; resuming restores it from the ${provider} archive.`);
  }
  if (rec.activity === 'unknown') {
    if (p.unknownActivity === 'refuse') {
      throw new ProviderError('provider_error',
        `${provider} cannot prove this conversation is not open elsewhere, and two processes resuming one conversation can corrupt it. `
        + `Close any other session using it, then run it from a shell outside Claude Code: ${manual}`);
    }
    if (!io.interactive) {
      throw new ProviderError('provider_error', `activity is unknown and no terminal is available to confirm; run it yourself: ${manual}`);
    }
    if (!/^y(es)?$/i.test((await io.ask('Activity of this conversation is unknown. Resume anyway? [y/N] ')).trim())) {
      throw new Cancelled('cancelled');
    }
  }
  const cwd = await chooseCwd(rec.cwd, io);
  if (rec.archived) {
    await p.unarchive(sessionId);
  }
  return resume(target, cwd, io);
}

async function isDir(path) {
  return Boolean(await stat(path).then((s) => s.isDirectory(), () => false));
}

// chooseCwd uses the recorded directory or asks for a replacement for this
// launch only; native metadata is not rewritten.
async function chooseCwd(cwd, io) {
  if (cwd && (await isDir(cwd))) {
    return cwd;
  }
  const problem = cwd ? `The recorded directory no longer exists: ${display(cwd)}` : 'The recorded directory is unknown.';
  if (!io.interactive) {
    throw new ProviderError('provider_error', `${problem} Rerun in a terminal to choose another directory.`);
  }
  io.say(problem);
  for (;;) {
    const answer = (await io.ask('Directory to resume in (empty to cancel): ')).trim();
    if (!answer) {
      throw new Cancelled('cancelled');
    }
    const dir = resolve(answer.replace(/^~(?=$|\/)/, homedir()));
    if (await isDir(dir)) {
      return dir;
    }
    io.say(`Not a directory: ${display(dir)}`);
  }
}

// resume runs the provider in the foreground. Terminal signals reach it
// directly; this process ignores SIGINT and forwards termination.
function resume({ command, args, clearEnv }, cwd, { spawn = nodeSpawn, env = process.env }) {
  const childEnv = { ...env };
  for (const name of clearEnv) {
    delete childEnv[name];
  }
  return new Promise((done, fail) => {
    const child = spawn(command, args, { cwd, env: childEnv, stdio: 'inherit' });
    const ignore = () => {};
    const forward = (signal) => child.kill(signal);
    const stop = () => {
      process.off('SIGINT', ignore);
      process.off('SIGTERM', forward);
      process.off('SIGHUP', forward);
    };
    process.on('SIGINT', ignore);
    process.on('SIGTERM', forward);
    process.on('SIGHUP', forward);
    child.on('error', (err) => {
      stop();
      fail(providerError(err, command));
    });
    child.on('exit', (code, signal) => {
      stop();
      done(code ?? 128 + (constants.signals[signal] ?? 0));
    });
  });
}
