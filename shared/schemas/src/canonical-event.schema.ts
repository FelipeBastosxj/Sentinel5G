import { z } from 'zod';
import { Channel, EventType } from '@eventstream/contracts';

/**
 * Zod schema mirror of {@link CanonicalEvent}.
 *
 * Used by the ingestion-service to validate every event before it touches
 * Kafka, enforcing HARDNESS §4 (canonical event rule) and §9 (security rule).
 */
export const canonicalEventSchema = z
  .object({
    eventId: z.string().uuid(),
    eventType: z.union([z.nativeEnum(EventType), z.string().min(1)]),
    channel: z.nativeEnum(Channel),
    timestamp: z
      .string()
      .refine((v) => Number.isFinite(Date.parse(v)), 'invalid ISO-8601 timestamp'),
    source: z.string().min(1),
    correlationId: z.string().min(1),
    version: z.string().min(1).optional(),
    metadata: z.record(z.unknown()).optional(),
    payload: z.unknown().optional(),
  })
  .strict();

export type CanonicalEventSchema = z.infer<typeof canonicalEventSchema>;
