// =============================================================================
// @telecom-webhook/contracts — public API
// =============================================================================

// Core event model
export {
  WebhookEvent,
  WebhookEventSummary,
  TelecomProvider,
  TelecomEventType,
  TelecomChannel,
} from './webhook-event.interface';

// Workspace entity
export { Workspace } from './workspace.interface';

// Twilio-specific provider payloads (used by the normalisation layer)
export {
  TwilioWebhookPayload,
  TwilioMessageStatus,
  TwilioCallStatus,
  TwilioCallDirection,
} from './provider-payloads';
