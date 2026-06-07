import { IngestEventUseCase } from './ingest-event.use-case';

describe('IngestEventUseCase', () => {
  function makeDeps() {
    const forwarder = { forward: jest.fn().mockResolvedValue(undefined) };
    const logger = {
      info: jest.fn(),
      debug: jest.fn(),
      warn: jest.fn(),
      error: jest.fn(),
    };
    return { forwarder, logger };
  }

  it('calls forwarder.forward with provider=twilio when X-Twilio-Signature is present', async () => {
    const { forwarder, logger } = makeDeps();
    const useCase = new IngestEventUseCase(forwarder as any, logger as any);

    await useCase.execute({
      workspaceId: 'ws-1',
      headers: { 'content-type': 'application/x-www-form-urlencoded', 'x-twilio-signature': 'sig' },
      body: { MessageSid: 'SM123', MessageStatus: 'delivered', From: '+1234', To: '+5678' },
    });

    expect(forwarder.forward).toHaveBeenCalledTimes(1);
    const captured1 = forwarder.forward.mock.calls[0][0];
    expect(captured1.workspaceId).toBe('ws-1');
    expect(captured1.provider).toBe('twilio');
  });

  it('calls forwarder.forward with provider=unknown when no provider header is present', async () => {
    const { forwarder, logger } = makeDeps();
    const useCase = new IngestEventUseCase(forwarder as any, logger as any);

    await useCase.execute({
      workspaceId: 'ws-2',
      headers: { 'content-type': 'application/json' },
      body: { foo: 'bar' },
    });

    expect(forwarder.forward).toHaveBeenCalledTimes(1);
    const captured2 = forwarder.forward.mock.calls[0][0];
    expect(captured2.provider).toBe('unknown');
  });

  it('propagates errors thrown by the forwarder', async () => {
    const { forwarder, logger } = makeDeps();
    forwarder.forward.mockRejectedValue(new Error('network error'));
    const useCase = new IngestEventUseCase(forwarder as any, logger as any);

    let thrown: Error | null = null;
    try {
      await useCase.execute({ workspaceId: 'ws-1', headers: {}, body: {} });
    } catch (err) {
      thrown = err as Error;
    }
    expect(thrown).not.toBeNull();
    expect(thrown?.message).toBe('network error');
  });
});
