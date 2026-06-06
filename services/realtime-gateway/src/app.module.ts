import { Module } from '@nestjs/common';
import { AppConfigModule } from './config/config.module';
import { CommonInfraModule } from './common/common-infra.module';
import { ObservabilityModule } from './common/observability.module';
import { RealtimeModule } from './realtime/realtime.module';

@Module({
  imports: [
    AppConfigModule,
    CommonInfraModule,
    ObservabilityModule,
    RealtimeModule,
  ],
})
export class AppModule {}
