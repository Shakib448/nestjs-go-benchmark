import { Module } from '@nestjs/common';
import { UserOverviewModule } from './modules/user/user-overview.module';
import { CheckoutModule } from './modules/checkout/checkout.module';
import { DatabaseModule } from './modules/database/database.module';
import { HealthModule } from './modules/health/health.module';

@Module({
    imports: [DatabaseModule, UserOverviewModule, CheckoutModule, HealthModule],
})
export class AppModule { }
