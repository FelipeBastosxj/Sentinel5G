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
    const env = {
      processingBaseUrl: 'http://processing:3003',
    };
    return { forwarder, logger, env };
  }

  it('should call forwarder.forward with correct data', async () => {
    const { forwarder, logger, env } = makeDeps();
    const useCase = new IngestEventUseCase(forwarder as any, logger as any, env as any);

    await useCase.execute({
      workspaceId: 'ws-1',
      headers: { 'content-type': 'application/x-www-form-urlencoded', 'x-twilio-signature': 'sig' },
      body: { MessageSid: 'SM123', MessageStatus: 'delivered', From: '+1234', To: '+5678' },
    });

    expect(forwarder.forward).toHaveBeenCalledWith(
      expect.objectContaining({
        workspaceId: 'ws-1',
        provider: 'twilio',
      }),
    );
  });

  it('should not throw when forwarder rejects (non-fatal)', async () => {
    const { forwarder, logger, env } = makeDeps();
    forwarder.forward.mockRejectedValue(new Error('network error'));
    const useCase = new IngestEventUseCase(forwarder as any, logger as any, env as any);

    await expect(
      useCase.execute({
        workspaceId: 'ws-1',
        headers: {},
        body: {},
      }),
    ).resolves.not.toThrow();
  });
});
