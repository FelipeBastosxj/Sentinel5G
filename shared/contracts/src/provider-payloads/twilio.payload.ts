/**
 * TwilioWebhookPayload — Raw shape of a Twilio status-callback webhook.
 *
 * Reference: https://www.twilio.com/docs/sms/api/message-resource#message-status-values
 *
 * NOTE: This type lives at the integration boundary. Per HARDNESS §5, it must
 *       NEVER cross into Kafka — webhook-service normalizes it into a
 *       CanonicalEvent before publishing.
 */
export interface TwilioWebhookPayload {
  MessageSid: string;
  AccountSid: string;
  From: string;
  To: string;
  Body?: string;
  MessageStatus: TwilioMessageStatus;
  ErrorCode?: string;
  ErrorMessage?: string;
  SmsStatus?: string;
  ApiVersion?: string;
  NumMedia?: string;
  NumSegments?: string;
}

export type TwilioMessageStatus =
  | 'queued'
  | 'sending'
  | 'sent'
  | 'delivered'
  | 'undelivered'
  | 'failed'
  | 'read'
  | 'received'
  | 'accepted'
  | 'scheduled'
  | 'canceled';
