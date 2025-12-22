import { Injectable, Inject } from '@nestjs/common';
import { Pool } from 'pg';
import Redis from 'ioredis';

interface User {
  id: string;
  plan: string;
  region: string;
  status: string;
}

interface Cart {
  id: string;
  status: string;
  updated_at: Date;
  cart_total: number;
  cart_items: number;
}

interface Order {
  id: string;
  status: string;
  total: number;
  created_at: Date;
  items_count: number;
}

interface Product {
  id: string;
  sku: string;
  price: number;
  available: number;
}

@Injectable()
export class UserOverviewService {
  constructor(
    @Inject('PG_POOL') private readonly db: Pool,
    @Inject('REDIS') private readonly redis: Redis,
  ) {}

  async getUserOverview(
    userId: string,
    categoryId: string | undefined,
    page: number,
    limit: number,
  ) {
    // 1) Validate user exists (DB light read or cached)
    let user = await this.getCachedUser(userId);
    if (!user) {
      const result = await this.db.query<User>(
        `SELECT id, plan, region, status FROM users WHERE id = $1 AND status = 'active'`,
        [userId],
      );
      if (result.rows.length === 0) {
        throw new Error('User not found');
      }
      user = result.rows[0];
      await this.redis.setex(`cache:user:${userId}`, 120, JSON.stringify(user));
    }

    // 2) Check summary cache (short TTL)
    const summaryKey = `cache:user:${userId}:summary:${
      categoryId || 'all'
    }:${page}:${limit}`;
    const cached = await this.redis.get(summaryKey);
    if (cached) {
      // Still do a tiny Redis op so it isn't "too easy"
      await this.redis.incr('metrics:get_overview_hits');
      return JSON.parse(cached);
    }

    // 3) Complex DB read (joins + aggregation + pagination)
    const [orders, cart, products] = await Promise.all([
      this.getRecentOrders(userId),
      this.getCurrentCart(userId),
      this.getRecommendedProducts(categoryId, page, limit),
    ]);

    // 4) Compute derived fields (CPU work)
    const orderTotalSum = orders.reduce((sum, o) => sum + Number(o.total), 0);
    const derived = {
      user_segment: this.computeSegment(user.plan, user.region, orderTotalSum),
      cart_age_seconds: cart
        ? Math.floor((Date.now() - new Date(cart.updated_at).getTime()) / 1000)
        : null,
      top_products: products.slice(0, 3).map((p) => p.id),
    };

    const response = { user, cart, orders, products, derived };

    // 5) Store summary cache, plus some extra redis ops
    await Promise.all([
      this.redis.setex(summaryKey, 30, JSON.stringify(response)),
      this.redis.sadd('metrics:active_users', userId),
      this.redis.expire('metrics:active_users', 3600),
    ]);

    return response;
  }

  private async getCachedUser(userId: string): Promise<User | null> {
    const cached = await this.redis.get(`cache:user:${userId}`);
    return cached ? JSON.parse(cached) : null;
  }

  private async getRecentOrders(userId: string): Promise<Order[]> {
    const result = await this.db.query<Order>(
      `SELECT o.id, o.status, o.total, o.created_at,
              COUNT(oi.product_id)::int as items_count
       FROM orders o
       JOIN order_items oi ON oi.order_id = o.id
       WHERE o.user_id = $1
       GROUP BY o.id
       ORDER BY o.created_at DESC
       LIMIT 10`,
      [userId],
    );
    return result.rows;
  }

  private async getCurrentCart(userId: string): Promise<Cart | null> {
    const result = await this.db.query<Cart>(
      `SELECT c.id, c.status, c.updated_at,
              COALESCE(SUM(ci.qty * ci.unit_price), 0)::decimal AS cart_total,
              COALESCE(SUM(ci.qty), 0)::int AS cart_items
       FROM carts c
       LEFT JOIN cart_items ci ON ci.cart_id = c.id
       WHERE c.user_id = $1 AND c.status = 'open'
       GROUP BY c.id
       LIMIT 1`,
      [userId],
    );
    return result.rows[0] || null;
  }

  private async getRecommendedProducts(
    categoryId: string | undefined,
    page: number,
    limit: number,
  ): Promise<Product[]> {
    const offset = (page - 1) * limit;
    const result = await this.db.query<Product>(
      `SELECT p.id, p.sku, p.price,
              COALESCE(SUM(i.available_qty - i.reserved_qty), 0)::int as available
       FROM products p
       LEFT JOIN inventory i ON i.product_id = p.id
       WHERE p.status = 'active'
         AND ($1::uuid IS NULL OR p.category_id = $1::uuid)
       GROUP BY p.id
       ORDER BY available DESC, p.id DESC
       OFFSET $2 LIMIT $3`,
      [categoryId || null, offset, limit],
    );
    return result.rows;
  }

  private computeSegment(
    plan: string,
    region: string,
    totalSpend: number,
  ): string {
    // Simulate some CPU work
    let hash = 0;
    const str = `${plan}:${region}:${totalSpend}`;
    for (let i = 0; i < str.length; i++) {
      hash = (hash << 5) - hash + str.charCodeAt(i);
      hash |= 0;
    }

    if (plan === 'enterprise' || totalSpend > 10000) return 'vip';
    if (plan === 'premium' || totalSpend > 5000) return 'premium';
    if (plan === 'basic' || totalSpend > 1000) return 'standard';
    return 'basic';
  }
}
