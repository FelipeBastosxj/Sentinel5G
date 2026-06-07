import { Global, Module, Injectable, OnModuleInit, OnModuleDestroy, Logger } from '@nestjs/common';
import { Client } from 'pg';
import { EnvService } from '../../config/config.module';

export const DATABASE_CLIENT = Symbol('DATABASE_CLIENT');

@Injectable()
export class DatabaseService implements OnModuleInit, OnModuleDestroy {
  private readonly logger = new Logger(DatabaseService.name);
  readonly client: Client;

  constructor(private readonly env: EnvService) {
    this.client = new Client({ connectionString: this.env.databaseUrl });
  }

  async onModuleInit(): Promise<void> {
    await this.client.connect();
    this.logger.log('PostgreSQL connected');
  }

  async onModuleDestroy(): Promise<void> {
    await this.client.end();
    this.logger.log('PostgreSQL disconnected');
  }
}

@Global()
@Module({
  providers: [
    DatabaseService,
    {
      provide: DATABASE_CLIENT,
      useFactory: (svc: DatabaseService) => svc.client,
      inject: [DatabaseService],
    },
  ],
  exports: [DatabaseService, DATABASE_CLIENT],
})
export class DatabaseModule {}
