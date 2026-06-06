import 'reflect-metadata';
import { startTracing } from './observability/tracing';

startTracing(process.env.OTEL_SERVICE_NAME_GATEWAY ?? 'realtime-gateway');

import { NestFactory } from '@nestjs/core';
import { Logger } from '@nestjs/common';
import { IoAdapter } from '@nestjs/platform-socket.io';
import { AppModule } from './app.module';
import { EnvService } from './config/config.module';
import type { ServerOptions } from 'socket.io';

class CustomIoAdapter extends IoAdapter {
  constructor(
    appOrHttpServer: unknown,
    private readonly options: { path: string; cors: { origin: string[] } },
  ) {
    super(appOrHttpServer);
  }
  createIOServer(port: number, options?: ServerOptions): unknown {
    return super.createIOServer(port, {
      ...(options ?? {}),
      path: this.options.path,
      cors: this.options.cors,
    } as ServerOptions);
  }
}

async function bootstrap(): Promise<void> {
  const app = await NestFactory.create(AppModule, { bufferLogs: true });
  app.enableShutdownHooks();
  const env = app.get(EnvService);

  app.useWebSocketAdapter(
    new CustomIoAdapter(app, {
      path: env.wsPath,
      cors: { origin: env.corsOrigin },
    }),
  );

  await app.listen(env.httpPort);
  Logger.log(
    `Realtime gateway listening on ws://localhost:${env.httpPort}${env.wsPath}`,
    'Bootstrap',
  );
}

bootstrap().catch((err) => {
  // eslint-disable-next-line no-console
  console.error('Fatal bootstrap error', err);
  process.exit(1);
});
