import { Injectable, Inject } from '@nestjs/common';
import { Pool, PoolClient } from 'pg';
import Redis from 'ioredis';
import { CheckoutDto } from './checkout.dto';
import { v4 as uuidv4 } from 'uuid';

interface CartItem {
  product_id: string;
  qty: number;
  unit_price: number;
  status: string;
}

interface Coupon {
  code: string;
  type: string;
  value: number;
  max_uses: number;
  used_count: number;
  starts_at: Date;
  ends_at: Date;
}

@Injectable()
export class CheckoutService {
  private readonly warehouseByRegion: Record<string, string> = {
    'us-east': '11111111-1111-1111-1111-111111111111',
    'us-west': '22222222-2222-2222-2222-222222222222',
    'eu-west': '33333333-3333-3333-3333-333333333333',
    'ap-southeast': '44444444-4444-4444-4444-444444444444',
  };

  constructor(
    @Inject('PG_POOL') private readonly db: Pool,
    @Inject('REDIS') private readonly redis: Redis,
  ) {}

  async checkout(dto: CheckoutDto) {
    const { userId, cartId, coupon: couponCode, paymentRef } = dto;
    const idempotencyKey = `idem:checkout:${paymentRef}`;

    // 0) Idempotency check (Redis)
    const existing = await this.redis.get(idempotencyKey);
    if (existing) {
      return JSON.parse(existing);
    }

    // 1) Rate limit (Redis)
    const currentMinute = Math.floor(Date.now() / 60000);
    const rlKey = `rl:user:${userId}:checkout:${currentMinute}`;
    const count = await this.redis.incr(rlKey);
    if (count === 1) {
      await this.redis.expire(rlKey, 90);
    }
    if (count > 10) {
      throw new Error('Rate limit exceeded');
    }

    // 2) Distributed lock (Redis)
    const lockKey = `lock:checkout:${userId}`;

    const locked = await this.redis.set(lockKey, '1', 'PX', 5000, 'NX');

    if (!locked) {
      throw new Error('Checkout in progress');
    }

    try {
      const result = await this.executeCheckoutTransaction(
        userId,
        cartId,
        couponCode,
      );

      // 5) Store idempotency response
      await this.redis.setex(idempotencyKey, 600, JSON.stringify(result));

      return result;
    } finally {
      await this.redis.del(lockKey);
    }
  }

  private async executeCheckoutTransaction(
    userId: string,
    cartId: string,
    couponCode?: string,
  ) {
    const client = await this.db.connect();

    try {
      await client.query('BEGIN');

      // 3.1) Validate cart ownership & open status (row lock)
      const cartResult = await client.query(
        `SELECT id, user_id, status FROM carts WHERE id = $1 AND user_id = $2 FOR UPDATE`,
        [cartId, userId],
      );
      if (
        cartResult.rows.length === 0 ||
        cartResult.rows[0].status !== 'open'
      ) {
        throw new Error('Cart not found or not open');
      }

      // 3.2) Load items from DB
      const itemsResult = await client.query<CartItem>(
        `SELECT ci.product_id, ci.qty, ci.unit_price, p.status
         FROM cart_items ci
         JOIN products p ON p.id = ci.product_id
         WHERE ci.cart_id = $1`,
        [cartId],
      );
      if (itemsResult.rows.length === 0) {
        throw new Error('Cart is empty');
      }
      const cartItems = itemsResult.rows;

      // 3.3) Coupon validation + usage lock
      let discount = 0;
      if (couponCode) {
        discount = await this.processCoupon(
          client,
          userId,
          couponCode,
          cartItems,
        );
      }

      // 3.4) Inventory reservation (lock rows)
      const warehouseId = await this.getWarehouseForUser(client, userId);
      await this.reserveInventory(client, cartItems, warehouseId);

      // 3.5) Compute totals
      const subtotal = cartItems.reduce(
        (sum, item) => sum + item.qty * Number(item.unit_price),
        0,
      );
      const tax = this.computeTax(subtotal - discount);
      const shipping = this.computeShipping(subtotal, cartItems.length);
      const total = Math.max(0, subtotal - discount + tax + shipping);

      // 3.6) Create order + items
      const orderId = await this.createOrder(
        client,
        userId,
        subtotal,
        discount,
        tax,
        shipping,
        total,
      );
      await this.createOrderItems(client, orderId, cartItems);

      // 3.7) Mark cart closed
      await client.query(
        `UPDATE carts SET status = 'closed', updated_at = NOW() WHERE id = $1`,
        [cartId],
      );

      // 3.8) Event log
      await client.query(
        `INSERT INTO events(user_id, type, payload_json, created_at)
         VALUES($1, 'ORDER_CREATED', $2, NOW())`,
        [userId, JSON.stringify({ orderId, total, cartId })],
      );

      await client.query('COMMIT');

      // 4) Post-commit Redis work
      await this.postCommitRedisOps(userId, orderId, total);

      return { orderId, status: 'pending', total };
    } catch (error) {
      await client.query('ROLLBACK');
      throw error;
    } finally {
      client.release();
    }
  }

  private async processCoupon(
    client: PoolClient,
    userId: string,
    couponCode: string,
    cartItems: CartItem[],
  ): Promise<number> {
    const couponResult = await client.query<Coupon>(
      `SELECT code, type, value, max_uses, used_count, starts_at, ends_at
       FROM coupons WHERE code = $1 FOR UPDATE`,
      [couponCode],
    );

    if (couponResult.rows.length === 0) {
      throw new Error('Invalid or expired coupon');
    }

    const coupon = couponResult.rows[0];
    const now = new Date();
    if (now < coupon.starts_at || now > coupon.ends_at) {
      throw new Error('Invalid or expired coupon');
    }
    if (coupon.max_uses && coupon.used_count >= coupon.max_uses) {
      throw new Error('Invalid or expired coupon');
    }

    // Check user usage
    const usageResult = await client.query(
      `SELECT used_count FROM user_coupon_usage WHERE user_id = $1 AND coupon_code = $2 FOR UPDATE`,
      [userId, couponCode],
    );

    if (usageResult.rows.length > 0 && usageResult.rows[0].used_count >= 1) {
      throw new Error('Coupon already used');
    }

    // Mark usage
    await client.query(
      `INSERT INTO user_coupon_usage(user_id, coupon_code, used_count)
       VALUES($1, $2, 1)
       ON CONFLICT(user_id, coupon_code)
       DO UPDATE SET used_count = user_coupon_usage.used_count + 1`,
      [userId, couponCode],
    );

    await client.query(
      `UPDATE coupons SET used_count = used_count + 1 WHERE code = $1`,
      [couponCode],
    );

    // Compute discount
    const subtotal = cartItems.reduce(
      (sum, item) => sum + item.qty * Number(item.unit_price),
      0,
    );
    if (coupon.type === 'percentage') {
      return subtotal * (Number(coupon.value) / 100);
    }
    return Number(coupon.value);
  }

  private async getWarehouseForUser(
    client: PoolClient,
    userId: string,
  ): Promise<string> {
    const result = await client.query<{ region: string }>(
      `SELECT region FROM users WHERE id = $1`,
      [userId],
    );
    const region = result.rows[0]?.region || 'us-east';
    return this.warehouseByRegion[region] || this.warehouseByRegion['us-east'];
  }

  private async reserveInventory(
    client: PoolClient,
    cartItems: CartItem[],
    warehouseId: string,
  ): Promise<void> {
    for (const item of cartItems) {
      const invResult = await client.query<{
        product_id: string;
        available_qty: number;
        reserved_qty: number;
      }>(
        `SELECT product_id, available_qty, reserved_qty
         FROM inventory
         WHERE product_id = $1 AND warehouse_id = $2
         FOR UPDATE`,
        [item.product_id, warehouseId],
      );

      if (invResult.rows.length === 0) {
        throw new Error('Insufficient inventory');
      }

      const inv = invResult.rows[0];
      if (inv.available_qty - inv.reserved_qty < item.qty) {
        throw new Error('Insufficient inventory');
      }

      await client.query(
        `UPDATE inventory
         SET reserved_qty = reserved_qty + $1, updated_at = NOW()
         WHERE product_id = $2 AND warehouse_id = $3`,
        [item.qty, item.product_id, warehouseId],
      );
    }
  }

  private computeTax(amount: number): number {
    return Math.round(amount * 0.08 * 100) / 100;
  }

  private computeShipping(subtotal: number, itemCount: number): number {
    if (subtotal > 100) return 0;
    return 5.99 + (itemCount - 1) * 0.99;
  }

  private async createOrder(
    client: PoolClient,
    userId: string,
    subtotal: number,
    discount: number,
    tax: number,
    shipping: number,
    total: number,
  ): Promise<string> {
    const result = await client.query<{ id: string }>(
      `INSERT INTO orders(id, user_id, status, subtotal, discount, tax, shipping, total, created_at)
       VALUES($1, $2, 'pending', $3, $4, $5, $6, $7, NOW())
       RETURNING id`,
      [uuidv4(), userId, subtotal, discount, tax, shipping, total],
    );
    return result.rows[0].id;
  }

  private async createOrderItems(
    client: PoolClient,
    orderId: string,
    cartItems: CartItem[],
  ): Promise<void> {
    const values = cartItems
      .map(
        (item, i) =>
          `($${i * 5 + 1}, $${i * 5 + 2}, $${i * 5 + 3}, $${i * 5 + 4}, $${
            i * 5 + 5
          })`,
      )
      .join(', ');

    const params = cartItems.flatMap((item) => [
      uuidv4(),
      orderId,
      item.product_id,
      item.qty,
      item.unit_price,
    ]);

    await client.query(
      `INSERT INTO order_items(id, order_id, product_id, qty, unit_price) VALUES ${values}`,
      params,
    );
  }

  private async postCommitRedisOps(
    userId: string,
    orderId: string,
    total: number,
  ): Promise<void> {
    const pipeline = this.redis.pipeline();

    // Scan and delete user summary cache keys (simplified - in production use SCAN)
    const keys = await this.redis.keys(`cache:user:${userId}:summary:*`);
    if (keys.length > 0) {
      pipeline.del(...keys);
    }

    pipeline.zincrby('leaderboard:top_buyers', total, userId);
    pipeline.xadd(
      'stream:order_events',
      '*',
      'userId',
      userId,
      'orderId',
      orderId,
      'total',
      String(total),
    );

    await pipeline.exec();
  }
}
