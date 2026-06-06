/**
 * SignatureValidatorPort — port for verifying that a webhook came from the
 * declared provider (HMAC, token, etc.). Each provider supplies its own.
 */
export interface SignatureValidatorPort {
  readonly providerName: string;

  /**
   * @param rawBody  the body exactly as bytes received (string for our usage).
   * @param headers  full incoming HTTP header bag (lowercase keys).
   * @param fullUrl  optional full URL the webhook hit (Twilio uses it).
   * @returns true when the signature matches; false to trigger a 401.
   */
  validate(
    rawBody: string,
    headers: Record<string, string | string[] | undefined>,
    fullUrl?: string,
  ): boolean;
}
