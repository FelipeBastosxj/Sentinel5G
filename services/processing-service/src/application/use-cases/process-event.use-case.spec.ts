import { Channel, EventType } from '@eventstream/contracts';
import { ProcessEventUseCase } from './process-event.use-case';

describe('ProcessEventUseCase', () => {
  const event = {
    eventId: '550e8400-e29b-41d4-a716-446655440000',
    eventType: EventType.DELIVERY_EVENT,
    channel: Channel.SMS,
    timestamp: '2026-06-05T12:00:00.000Z',
    source: 'twilio',
    correlationId: 'corr-1',
    payload: { to: '+1 (555) 000-0001', from: '+15550000099' },
  };

  function makeDeps(publishImpl?: jest.Mock) {
    const publishMock = publishImpl ?? jest.fn().mockResolvedValue(undefined);
    return {
      enricher: { enrich: jest.fn((e) => e) },
      publisher: { publish: publishMock },
      retry: {
        run: jest.fn(async (fn: () => Promise<unknown>) => fn()),
      },
      env: { retryAttempts: 3, retryBaseDelayMs: 10 },
      metrics: {
        eventsConsumedTotal: { inc: jest.fn() },
        eventsProcessedTotal: { inc: jest.fn() },
        eventsRetriedTotal: { inc: jest.fn() },
        eventsDeadLetteredTotal: { inc: jest.fn() },
        processingDurationSeconds: {
          startTimer: jest.fn(() => () => undefined),
        },
      },
      logger: {
        info: jest.fn(),
        warn: jest.fn(),
        error: jest.fn(),
        debug: jest.fn(),
      },
    };
  }

  it('publishes to events.processed and emits a metric event', async () => {
    const deps = makeDeps();
    const uc = new ProcessEventUseCase(
      deps.enricher as never,
      deps.publisher as never,
      deps.retry as never,
      deps.env as never,
      deps.metrics as never,
      deps.logger as never,
    );

    await uc.execute(event as never);

    const calls = deps.publisher.publish.mock.calls.map((c) => c[0]);
    expect(calls).toContain('events.processed');
    expect(calls).toContain('events.metrics');
    expect(deps.metrics.eventsProcessedTotal.inc).toHaveBeenCalled();
  });

  it('routes to DLQ when retries are exhausted', async () => {
    let processedAttempted = false;
    const publish = jest.fn().mockImplementation(async (topic: string) => {
      if (topic === 'events.processed') {
        processedAttempted = true;
        throw new Error('kafka down');
      }
      // alerts publish succeeds
    });
    const deps = makeDeps(publish);
    deps.retry.run.mockImplementation(
      async (fn: () => Promise<unknown>) => fn(),
    );

    const uc = new ProcessEventUseCase(
      deps.enricher as never,
      deps.publisher as never,
      deps.retry as never,
      deps.env as never,
      deps.metrics as never,
      deps.logger as never,
    );

    await uc.execute(event as never);

    expect(processedAttempted).toBe(true);
    const topics = deps.publisher.publish.mock.calls.map((c) => c[0]);
    expect(topics).toContain('events.alerts');
    expect(deps.metrics.eventsDeadLetteredTotal.inc).toHaveBeenCalled();
  });
});
