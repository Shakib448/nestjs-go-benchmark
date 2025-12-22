import {
  Controller,
  Get,
  Param,
  Query,
  HttpException,
  HttpStatus,
} from '@nestjs/common';
import { UserOverviewService } from './user-overview.service';

@Controller('v1/users')
export class UserOverviewController {
  constructor(private readonly service: UserOverviewService) {}

  @Get(':userId/overview')
  async getUserOverview(
    @Param('userId') userId: string,
    @Query('categoryId') categoryId?: string,
    @Query('page') page?: string,
    @Query('limit') limit?: string,
  ) {
    const pageNum = parseInt(page || '1', 10);
    const limitNum = parseInt(limit || '10', 10);

    try {
      return await this.service.getUserOverview(
        userId,
        categoryId,
        pageNum,
        limitNum,
      );
    } catch (error) {
      if (error.message === 'User not found') {
        throw new HttpException('User not found', HttpStatus.NOT_FOUND);
      }
      throw error;
    }
  }
}
