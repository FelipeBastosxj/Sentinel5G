// =============================================================================
// @telecom-webhook/contracts — public API
// =============================================================================

// Core event model
export {
  WebhookEvent,
  WebhookEventSummary,
  TelecomProvider,
  TelecomEventType,
} from './webhook-event.interface';

// Twilio-specific provider payloads (used by normalisation layer)
export {
  TwilioWebhookPayload,
  TwilioMessageStatus,
} from './provider-payloads';
