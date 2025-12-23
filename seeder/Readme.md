# For schema write into database
psql -h localhost -p 5434 -U postgres -d loadtest -f schema.sql
