import { Module } from '@nestjs/common';
import { HealthController } from './health.controller';
import { KafkaModule } from '../../infrastructure/kafka/kafka.module';

@Module({
  imports: [KafkaModule],
  controllers: [HealthController],
})
export class HealthModule {}
