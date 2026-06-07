import 'reflect-metadata';
import { NestFactory } from '@nestjs/core';
import { NestExpressApplication } from '@nestjs/platform-express';
import { Logger } from '@nestjs/common';
import { json, urlencoded } from 'express';
import { AppModule } from './app.module';
import { EnvService } from './config/config.module';

async function bootstrap(): Promise<void> {
  const app = await NestFactory.create<NestExpressApplication>(AppModule, {
    bufferLogs: true,
    bodyParser: false, // configured explicitly below
  });
  app.enableShutdownHooks();

  app.set('trust proxy', true);
  app.use(json({ limit: '5mb' }));
  app.use(urlencoded({ extended: true, limit: '5mb' }));

  app.enableCors({ origin: '*', methods: ['GET', 'POST'] });

  const env = app.get(EnvService);
  await app.listen(env.httpPort);

  Logger.log(
    `Processing service listening on http://localhost:${env.httpPort}`,
    'Bootstrap',
  );
}

bootstrap().catch((err) => {
  // eslint-disable-next-line no-console
  console.error('Fatal bootstrap error', err);
  process.exit(1);
});
