/**
 * Supported telecom webhook providers.
 *
 * `'unknown'` is used when a webhook arrives without any recognisable
 * provider signature (neither header nor body fields). The event is still
 * persisted with the raw payload for debugging.
 */
export type TelecomProvider =
  | 'twilio'
  | 'vonage'
  | 'messagebird'
  | 'infobip'
  | 'plivo'
  | 'unknown';

/**
 * Logical messaging channel inferred from the provider payload.
 *
 *  - 'sms'      — short message service (default for any message-shaped payload)
 *  - 'whatsapp' — Twilio WhatsApp business API (From/To prefixed with 'whatsapp:')
 *  - 'voice'    — call control (presence of CallSid)
 *  - 'other'    — none of the above (e.g. provider verification ping, generic webhook)
 */
export type TelecomChannel = 'sms' | 'whatsapp' | 'voice' | 'other';

/**
 * High-level event categories for telecom webhooks.
 */
export type TelecomEventType =
  // SMS / WhatsApp messaging
  | 'message.inbound'
  | 'message.status.queued'
  | 'message.status.sent'
  | 'message.status.delivered'
  | 'message.status.undelivered'
  | 'message.status.failed'
  | 'message.status.read'
  // Voice
  | 'call.inbound'
  | 'call.outbound'
  | 'call.status.initiated'
  | 'call.status.ringing'
  | 'call.status.in-progress'
  | 'call.status.completed'
  | 'call.status.busy'
  | 'call.status.no-answer'
  | 'call.status.failed'
  // Generic fallback
  | (string & NonNullable<unknown>);

/**
 * WebhookEvent — the core data model of the entire platform.
 *
 * Every inbound webhook from any telecom provider is normalised into this
 * structure before being persisted and broadcast to real-time clients.
 *
 * Rules:
 *  - `id` and `workspaceId` are assigned server-side (UUID v4).
 *  - `receivedAt` is stamped at the HTTP layer before any processing.
 *  - `headers` and `payload` are stored verbatim (raw capture).
 *  - Provider-specific fields (`messageSid`, `callSid`, etc.) are extracted
 *    during normalisation and indexed for fast filtering.
 */
export interface WebhookEvent {
  /** Server-assigned unique identifier (UUID v4). */
  readonly id: string;

  /** The workspace this event was delivered to. */
  readonly workspaceId: string;

  /** The telecom provider that sent this webhook. */
  readonly provider: TelecomProvider;

  /** Normalised event type. */
  readonly eventType: TelecomEventType;

  /** Logical messaging channel (sms, whatsapp, voice, other). */
  readonly channel?: TelecomChannel;

  /** UTC timestamp when the HTTP request was received. */
  readonly receivedAt: Date;

  /** All HTTP request headers, lower-cased. */
  readonly headers: Readonly<Record<string, string>>;

  /** Raw parsed body of the HTTP request. */
  readonly payload: Readonly<Record<string, unknown>>;

  // ── Telecom-specific extracted fields (all optional) ────────────────────

  /** Twilio MessageSid / provider equivalent message identifier. */
  readonly messageSid?: string;

  /** Twilio CallSid / provider equivalent call identifier. */
  readonly callSid?: string;

  /** Originating phone number (E.164). */
  readonly from?: string;

  /** Destination phone number (E.164). */
  readonly to?: string;

  /** Delivery / call status reported by the provider. */
  readonly status?: string;

  // ── Server-side processing fields ────────────────────────────────────────

  /** UTC timestamp when the event finished processing (null = pending). */
  readonly processedAt?: Date;

  /** Total wall-clock processing time in milliseconds. */
  readonly processingMs?: number;
}

/**
 * Lightweight version of WebhookEvent used in list responses
 * and real-time stream payloads (omits verbose fields).
 */
export type WebhookEventSummary = Pick<
  WebhookEvent,
  | 'id'
  | 'workspaceId'
  | 'provider'
  | 'eventType'
  | 'channel'
  | 'receivedAt'
  | 'messageSid'
  | 'callSid'
  | 'from'
  | 'to'
  | 'status'
  | 'processingMs'
>;
