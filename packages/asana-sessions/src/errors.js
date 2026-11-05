// UsageError is a malformed command or request; it exits 2.
export class UsageError extends Error {}

// ProviderError carries a spec 16 diagnostic code: unavailable,
// unsupported_version, permission_denied, metadata_conflict, incomplete, or
// provider_error. Messages are short and never include protocol output.
// missing marks history that does not exist at all, as opposed to a failure.
export class ProviderError extends Error {
  constructor(code, message, { missing = false } = {}) {
    super(message);
    this.code = code;
    this.missing = missing;
  }
}

// providerError maps a filesystem or spawn failure to a ProviderError.
export function providerError(err, what) {
  if (err instanceof ProviderError) {
    return err;
  }
  switch (err?.code) {
    case 'ENOENT':
      return new ProviderError('unavailable', `${what} is not installed or has no history`, { missing: true });
    case 'EACCES':
    case 'EPERM':
      return new ProviderError('permission_denied', `${what} history is not readable`);
    default:
      return new ProviderError('provider_error', `${what} failed: ${String(err?.message ?? err).split('\n')[0].slice(0, 300)}`);
  }
}
