/**
 * Domain-level error raised when a provider webhook signature fails verification.
 * Translated to HTTP 401 by the global exception filter (HARDNESS §9).
 */
export class InvalidSignatureError extends Error {
  constructor(public readonly provider: string, message?: string) {
    super(message ?? `Invalid signature for provider "${provider}"`);
    this.name = 'InvalidSignatureError';
  }
}
