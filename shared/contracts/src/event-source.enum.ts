/**
 * EventSource — Known origin systems / providers.
 *
 * The `source` field of a CanonicalEvent is typed as `EventSource | string` so
 * unknown providers (custom integrations) remain accepted, while well-known
 * providers benefit from autocomplete and refactor-safety.
 */
export enum EventSource {
  TWILIO = 'twilio',
  INFOBIP = 'infobip',
  SENDGRID = 'sendgrid',
  CUSTOM = 'custom',
  INTERNAL = 'internal',
}
