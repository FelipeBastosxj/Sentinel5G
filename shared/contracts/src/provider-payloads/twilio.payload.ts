/**
 * TwilioWebhookPayload — Raw shape of a Twilio webhook (SMS or Voice).
 *
 * Twilio sends `application/x-www-form-urlencoded` for all webhooks.
 * The same logical field may arrive under multiple legacy aliases:
 *   - MessageSid / SmsSid / SmsMessageSid  (all the same value)
 *   - MessageStatus / SmsStatus            (delivery status)
 *   - From / Caller                        (originating number)
 *   - To / Called                          (destination number)
 *
 * Reference:
 *   https://www.twilio.com/docs/messaging/guides/webhook-request
 *   https://www.twilio.com/docs/voice/twiml#request-parameters
 */
export interface TwilioWebhookPayload {
  // ── Identifiers ────────────────────────────────────────────────────────
  AccountSid: string;            // always starts with 'AC'
  MessageSid?: string;
  SmsSid?: string;               // legacy alias of MessageSid
  SmsMessageSid?: string;        // legacy alias of MessageSid
  CallSid?: string;
  MessagingServiceSid?: string;

  // ── Numbers ────────────────────────────────────────────────────────────
  From?: string;                 // E.164
  To?: string;                   // E.164
  Caller?: string;               // voice alias of From
  Called?: string;               // voice alias of To

  // ── Messaging ──────────────────────────────────────────────────────────
  Body?: string;                 // SMS / WhatsApp text
  NumMedia?: string;             // number of MMS attachments
  NumSegments?: string;
  MessageStatus?: TwilioMessageStatus;
  SmsStatus?: string;            // legacy alias of MessageStatus

  // ── Voice ──────────────────────────────────────────────────────────────
  CallStatus?: TwilioCallStatus;
  Direction?: TwilioCallDirection;
  CallDuration?: string;
  ForwardedFrom?: string;

  // ── Errors (delivery callbacks) ────────────────────────────────────────
  ErrorCode?: string;
  ErrorMessage?: string;

  // ── Misc ───────────────────────────────────────────────────────────────
  ApiVersion?: string;
}

export type TwilioMessageStatus =
  | 'accepted'
  | 'queued'
  | 'scheduled'
  | 'sending'
  | 'sent'
  | 'delivered'
  | 'undelivered'
  | 'failed'
  | 'read'
  | 'received'
  | 'canceled';

export type TwilioCallStatus =
  | 'queued'
  | 'ringing'
  | 'in-progress'
  | 'completed'
  | 'busy'
  | 'failed'
  | 'no-answer'
  | 'canceled';

export type TwilioCallDirection =
  | 'inbound'
  | 'outbound-api'
  | 'outbound-dial';
