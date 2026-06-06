import { Controller, Get } from '@nestjs/common';
import { KafkaProducerAdapter } from '../../infrastructure/kafka/kafka-producer.adapter';

interface HealthResponse {
  readonly status: 'ok' | 'degraded';
  readonly checks: {
    readonly kafka: 'connected' | 'disconnected';
  };
  readonly uptimeSeconds: number;
}

@Controller('health')
export class HealthController {
  constructor(private readonly kafkaProducer: KafkaProducerAdapter) {}

  @Get()
  check(): HealthResponse {
    const kafka = this.kafkaProducer.isConnected() ? 'connected' : 'disconnected';
    return {
      status: kafka === 'connected' ? 'ok' : 'degraded',
      checks: { kafka },
      uptimeSeconds: Math.floor(process.uptime()),
    };
  }
}
