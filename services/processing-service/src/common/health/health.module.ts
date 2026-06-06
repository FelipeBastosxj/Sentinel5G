import { Controller, Get, Module } from '@nestjs/common';
import { KafkaConsumerAdapter } from '../../infrastructure/kafka/kafka-consumer.adapter';
import { KafkaProducerAdapter } from '../../infrastructure/kafka/kafka-producer.adapter';
import { KafkaModule } from '../../infrastructure/kafka/kafka.module';

@Controller('health')
export class HealthController {
  constructor(
    private readonly consumer: KafkaConsumerAdapter,
    private readonly producer: KafkaProducerAdapter,
  ) {}

  @Get()
  check(): {
    status: 'ok' | 'degraded';
    checks: { producer: string; consumer: string };
    uptimeSeconds: number;
  } {
    const producer = this.producer.isConnected() ? 'connected' : 'disconnected';
    const consumer = this.consumer.isRunning() ? 'running' : 'stopped';
    return {
      status:
        producer === 'connected' && consumer === 'running' ? 'ok' : 'degraded',
      checks: { producer, consumer },
      uptimeSeconds: Math.floor(process.uptime()),
    };
  }
}

@Module({
  imports: [KafkaModule],
  controllers: [HealthController],
})
export class HealthModule {}
