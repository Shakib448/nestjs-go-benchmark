import {
  Controller,
  Post,
  Body,
  HttpException,
  HttpStatus,
} from '@nestjs/common';
import { CheckoutService } from './checkout.service';
import { CheckoutDto } from './checkout.dto';

@Controller('v1')
export class CheckoutController {
  constructor(private readonly service: CheckoutService) {}

  @Post('checkout')
  async checkout(@Body() dto: CheckoutDto) {
    try {
      return await this.service.checkout(dto);
    } catch (error) {
      const statusMap: Record<string, HttpStatus> = {
        'Rate limit exceeded': HttpStatus.TOO_MANY_REQUESTS,
        'Checkout in progress': HttpStatus.CONFLICT,
        'Cart not found or not open': HttpStatus.BAD_REQUEST,
        'Cart is empty': HttpStatus.BAD_REQUEST,
        'Invalid or expired coupon': HttpStatus.BAD_REQUEST,
        'Coupon already used': HttpStatus.BAD_REQUEST,
        'Insufficient inventory': HttpStatus.CONFLICT,
      };

      const status =
        statusMap[error.message] || HttpStatus.INTERNAL_SERVER_ERROR;
      throw new HttpException(error.message, status);
    }
  }
}
