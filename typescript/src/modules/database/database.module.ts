import { Global, Module } from '@nestjs/common';
import { Pool } from 'pg';
import Redis from 'ioredis';

@Global()
@Module({
  providers: [
    {
      provide: 'PG_POOL',
      useFactory: async () => {
        const pool = new Pool({
          host: process.env.DB_HOST || 'localhost',
          port: Number(process.env.DB_PORT || '5433'),
          database: process.env.DB_NAME || 'loadtest',
          user: process.env.DB_USER || 'postgres',
          password: process.env.DB_PASSWORD || 'postgres',
          max: 20,
        });

        // 🔑 connection test (prevents silent hang)
        await pool.query('select 1');
        console.log('✅ PostgreSQL connected');

        return pool;
      },
    },
    {
      provide: 'REDIS',
      useFactory: async () => {
        const redis = new Redis({
          host: process.env.REDIS_HOST || 'localhost',
          port: Number(process.env.REDIS_PORT || 6380),
          maxRetriesPerRequest: 3,
        });

        await redis.ping();
        console.log('✅ Redis connected');

        return redis;
      },
    },
  ],
  exports: ['PG_POOL', 'REDIS'],
})
export class DatabaseModule {}
