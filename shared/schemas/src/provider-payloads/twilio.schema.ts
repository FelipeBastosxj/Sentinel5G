import { z } from 'zod';

/**
 * Zod schema for a Twilio status-callback webhook payload.
 * Twilio posts as `application/x-www-form-urlencoded`, so all fields are strings.
 */
export const twilioWebhookSchema = z
  .object({
    MessageSid: z.string().min(1),
    AccountSid: z.string().min(1),
    From: z.string().min(1),
    To: z.string().min(1),
    Body: z.string().optional(),
    MessageStatus: z.enum([
      'queued',
      'sending',
      'sent',
      'delivered',
      'undelivered',
      'failed',
      'read',
      'received',
      'accepted',
      'scheduled',
      'canceled',
    ]),
    ErrorCode: z.string().optional(),
    ErrorMessage: z.string().optional(),
    SmsStatus: z.string().optional(),
    ApiVersion: z.string().optional(),
    NumMedia: z.string().optional(),
    NumSegments: z.string().optional(),
  })
  .passthrough();

export type TwilioWebhookSchema = z.infer<typeof twilioWebhookSchema>;
