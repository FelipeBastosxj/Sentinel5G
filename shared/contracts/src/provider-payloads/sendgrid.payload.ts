/**
 * SendGridWebhookPayload — Raw shape of a SendGrid event webhook batch.
 *
 * Reference: https://docs.sendgrid.com/for-developers/tracking-events/event
 *
 * Lives at the integration boundary. HARDNESS §5: must never enter Kafka raw.
 */
export type SendGridWebhookPayload = SendGridEvent[];

export interface SendGridEvent {
  email: string;
  timestamp: number; // Unix seconds
  event: SendGridEventType;
  sg_event_id?: string;
  sg_message_id?: string;
  smtp_id?: string;
  reason?: string;
  status?: string;
  response?: string;
  category?: string | string[];
  url?: string;
  ip?: string;
  useragent?: string;
}

export type SendGridEventType =
  | 'processed'
  | 'deferred'
  | 'delivered'
  | 'open'
  | 'click'
  | 'bounce'
  | 'dropped'
  | 'spamreport'
  | 'unsubscribe'
  | 'group_unsubscribe'
  | 'group_resubscribe';
