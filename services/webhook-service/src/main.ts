import 'reflect-metadata';
import { startTracing } from './observability/tracing';

startTracing(process.env.OTEL_SERVICE_NAME_WEBHOOK ?? 'webhook-service');

import { NestFactory } from '@nestjs/core';
import { Logger } from '@nestjs/common';
import { json, urlencoded } from 'express';
import { AppModule } from './app.module';
import { EnvService } from './config/env.service';
import * as bodyParser from 'body-parser';
import { IncomingMessage } from 'http';

async function bootstrap(): Promise<void> {
  const app = await NestFactory.create(AppModule, { bufferLogs: true });
  app.enableShutdownHooks();

  app.use(
    bodyParser.urlencoded({
      extended: true,
      verify: (req: IncomingMessage & { rawBody?: string }, _res, buf) => {
        req.rawBody = buf.toString('utf8');
      },
    }),
  );
  app.use(
    bodyParser.json({
      verify: (req: IncomingMessage & { rawBody?: string }, _res, buf) => {
        req.rawBody = buf.toString('utf8');
      },
    }),
  );

  const env = app.get(EnvService);
  await app.listen(env.httpPort);
  Logger.log(
    `Webhook service listening on http://localhost:${env.httpPort}`,
    'Bootstrap',
  );
}

bootstrap().catch((err) => {
  // eslint-disable-next-line no-console
  console.error('Fatal bootstrap error', err);
  process.exit(1);
});
