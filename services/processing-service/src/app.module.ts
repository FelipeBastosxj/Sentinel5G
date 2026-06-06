import { Module } from '@nestjs/common';
import { AppConfigModule } from './config/config.module';
import { ProcessingModule } from './processing/processing.module';

@Module({
  imports: [AppConfigModule, ProcessingModule],
})
export class AppModule {}
