import { CanonicalEvent } from '@eventstream/contracts';

/**
 * NormalizationContext — request-scoped data the normalizer needs to enrich
 * the resulting CanonicalEvent (correlationId comes from middleware).
 */
export interface NormalizationContext {
  readonly correlationId: string;
}

/**
 * ProviderNormalizerPort — domain port mapping a raw provider payload to one
 * or more CanonicalEvents.
 *
 * Concrete normalizers live in `domain/normalizers/<provider>.normalizer.ts`.
 */
export interface ProviderNormalizerPort<RawPayload = unknown> {
  readonly providerName: string;
  normalize(
    payload: RawPayload,
    context: NormalizationContext,
  ): CanonicalEvent[];
}
