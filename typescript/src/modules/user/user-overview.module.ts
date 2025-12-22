import { Module } from '@nestjs/common';
import { UserOverviewController } from './user-overview.controller';
import { UserOverviewService } from './user-overview.service';

@Module({
  controllers: [UserOverviewController],
  providers: [UserOverviewService],
})
export class UserOverviewModule {}
