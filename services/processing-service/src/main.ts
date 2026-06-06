import 'reflect-metadata';
import { startTracing } from './observability/tracing';

startTracing(process.env.OTEL_SERVICE_NAME_PROCESSING ?? 'processing-service');

import { NestFactory } from '@nestjs/core';
import { Logger } from '@nestjs/common';
import { AppModule } from './app.module';
import { EnvService } from './config/config.module';

async function bootstrap(): Promise<void> {
  const app = await NestFactory.create(AppModule, { bufferLogs: true });
  app.enableShutdownHooks();
  const env = app.get(EnvService);
  await app.listen(env.httpPort);
  Logger.log(
    `Processing service listening on http://localhost:${env.httpPort} (consuming events.raw)`,
    'Bootstrap',
  );
}

bootstrap().catch((err) => {
  // eslint-disable-next-line no-console
  console.error('Fatal bootstrap error', err);
  process.exit(1);
});
