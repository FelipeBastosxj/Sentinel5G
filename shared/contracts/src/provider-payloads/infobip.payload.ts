/**
 * InfobipWebhookPayload — Raw shape of an Infobip delivery-report webhook.
 *
 * Reference: https://www.infobip.com/docs/api/channels/sms/inbound-sms/receive-outbound-sms-message-report
 *
 * Lives at the integration boundary. HARDNESS §5: must never enter Kafka raw.
 */
export interface InfobipWebhookPayload {
  results: InfobipDeliveryReport[];
}

export interface InfobipDeliveryReport {
  bulkId?: string;
  messageId: string;
  to: string;
  from?: string;
  sentAt?: string;
  doneAt?: string;
  smsCount?: number;
  mccMnc?: string;
  price?: { pricePerMessage?: number; currency?: string };
  status: {
    id: number;
    groupId: number;
    groupName: InfobipStatusGroup;
    name: string;
    description?: string;
  };
  error?: {
    id: number;
    name: string;
    description?: string;
    permanent?: boolean;
  };
}

export type InfobipStatusGroup =
  | 'PENDING'
  | 'UNDELIVERABLE'
  | 'DELIVERED'
  | 'EXPIRED'
  | 'REJECTED';
