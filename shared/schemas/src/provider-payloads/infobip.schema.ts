import { z } from 'zod';

const infobipDeliveryReportSchema = z.object({
  bulkId: z.string().optional(),
  messageId: z.string().min(1),
  to: z.string().min(1),
  from: z.string().optional(),
  sentAt: z.string().optional(),
  doneAt: z.string().optional(),
  smsCount: z.number().int().nonnegative().optional(),
  mccMnc: z.string().optional(),
  price: z
    .object({
      pricePerMessage: z.number().optional(),
      currency: z.string().optional(),
    })
    .optional(),
  status: z.object({
    id: z.number().int(),
    groupId: z.number().int(),
    groupName: z.enum([
      'PENDING',
      'UNDELIVERABLE',
      'DELIVERED',
      'EXPIRED',
      'REJECTED',
    ]),
    name: z.string(),
    description: z.string().optional(),
  }),
  error: z
    .object({
      id: z.number().int(),
      name: z.string(),
      description: z.string().optional(),
      permanent: z.boolean().optional(),
    })
    .optional(),
});

export const infobipWebhookSchema = z
  .object({
    results: z.array(infobipDeliveryReportSchema).min(1),
  })
  .passthrough();

export type InfobipWebhookSchema = z.infer<typeof infobipWebhookSchema>;
