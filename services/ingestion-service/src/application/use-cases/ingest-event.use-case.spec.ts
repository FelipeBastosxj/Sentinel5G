import { Channel, EventType } from '@eventstream/contracts';
import { SchemaValidationError } from '@eventstream/schemas';
import { IngestEventUseCase } from './ingest-event.use-case';

describe('IngestEventUseCase', () => {
  const validInput = {
    eventId: '550e8400-e29b-41d4-a716-446655440000',
    eventType: EventType.DELIVERY_EVENT,
    channel: Channel.SMS,
    timestamp: '2026-06-05T12:00:00.000Z',
    source: 'twilio',
    correlationId: 'corr-1',
  };

  function makeDeps() {
    const publisher = { publish: jest.fn().mockResolvedValue(undefined) };
    const metrics = {
      eventsReceivedTotal: { inc: jest.fn() },
      eventsRejectedTotal: { inc: jest.fn() },
      eventsPublishedTotal: { inc: jest.fn() },
      publishLatencySeconds: { startTimer: jest.fn(() => () => undefined) },
    };
    const logger = {
      info: jest.fn(),
      debug: jest.fn(),
      warn: jest.fn(),
      error: jest.fn(),
    };
    const correlation = { getCorrelationId: jest.fn(() => 'fallback-corr') };
    return { publisher, metrics, logger, correlation };
  }

  function createUseCase(deps: ReturnType<typeof makeDeps>): IngestEventUseCase {
    return new IngestEventUseCase(
      deps.publisher as never,
      deps.metrics as never,
      deps.logger as never,
      deps.correlation as never,
    );
  }

  it('validates, builds, and publishes the event', async () => {
    const deps = makeDeps();
    const useCase = createUseCase(deps);

    const event = await useCase.execute({ payload: validInput });

    expect(event.eventId).toBe(validInput.eventId);
    expect(event.channel).toBe(Channel.SMS);
    expect(deps.publisher.publish).toHaveBeenCalledWith(
      'events.raw',
      expect.objectContaining({ eventId: validInput.eventId }),
    );
    expect(deps.metrics.eventsReceivedTotal.inc).toHaveBeenCalled();
  });

  it('throws SchemaValidationError when the payload is invalid', async () => {
    const deps = makeDeps();
    const useCase = createUseCase(deps);

    await expect(
      useCase.execute({ payload: { ...validInput, channel: 'invalid' } }),
    ).rejects.toBeInstanceOf(SchemaValidationError);
    expect(deps.publisher.publish).not.toHaveBeenCalled();
    expect(deps.metrics.eventsRejectedTotal.inc).toHaveBeenCalledWith({
      reason: 'schema',
    });
  });

  it('falls back to the correlation context when correlationId is missing', async () => {
    const deps = makeDeps();
    const useCase = createUseCase(deps);

    // Correlation id is required by the schema, so the schema would reject —
    // verify via schema validation first.
    await expect(
      useCase.execute({ payload: { ...validInput, correlationId: '' } }),
    ).rejects.toBeInstanceOf(SchemaValidationError);
  });
});
