import 'reflect-metadata';
import { NestFactory } from '@nestjs/core';
import { NestExpressApplication } from '@nestjs/platform-express';
import { Logger, ValidationPipe } from '@nestjs/common';
import { json, urlencoded } from 'express';
import { AppModule } from './app.module';
import { EnvService } from './config/env.service';

async function bootstrap(): Promise<void> {
  const app = await NestFactory.create<NestExpressApplication>(AppModule, {
    bufferLogs: true,
    cors: false,
    bodyParser: false, // configured explicitly below
  });
  app.enableShutdownHooks();

  // Trust X-Forwarded-* headers from cloudflared / nginx / load balancers,
  // so that req.ip and req.protocol reflect the original client.
  app.set('trust proxy', true);

  // Explicit body parsers — Twilio sends application/x-www-form-urlencoded;
  // some providers send JSON. Generous limit covers MMS metadata.
  app.use(json({ limit: '5mb' }));
  app.use(urlencoded({ extended: true, limit: '5mb' }));

  app.useGlobalPipes(
    new ValidationPipe({
      whitelist: true,
      forbidNonWhitelisted: false,
      transform: false,
    }),
  );

  const env = app.get(EnvService);
  await app.listen(env.httpPort);

  Logger.log(
    `Ingestion service listening on http://localhost:${env.httpPort}`,
    'Bootstrap',
  );
}

bootstrap().catch((err) => {
  // eslint-disable-next-line no-console
  console.error('Fatal bootstrap error', err);
  process.exit(1);
});
