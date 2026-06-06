import { z } from 'zod';

const sendGridEventSchema = z
  .object({
    email: z.string().email(),
    timestamp: z.number().int().nonnegative(),
    event: z.enum([
      'processed',
      'deferred',
      'delivered',
      'open',
      'click',
      'bounce',
      'dropped',
      'spamreport',
      'unsubscribe',
      'group_unsubscribe',
      'group_resubscribe',
    ]),
    sg_event_id: z.string().optional(),
    sg_message_id: z.string().optional(),
    smtp_id: z.string().optional(),
    reason: z.string().optional(),
    status: z.string().optional(),
    response: z.string().optional(),
    category: z.union([z.string(), z.array(z.string())]).optional(),
    url: z.string().optional(),
    ip: z.string().optional(),
    useragent: z.string().optional(),
  })
  .passthrough();

export const sendgridWebhookSchema = z.array(sendGridEventSchema).min(1);

export type SendGridWebhookSchema = z.infer<typeof sendgridWebhookSchema>;
