# Web API Reference

The Simple Trading Bot provides a comprehensive REST API for monitoring, management, and integration with external systems.

## 🌐 Base Configuration

- **Base URL**: `http://localhost:8080` (configurable via `--port`)
- **Authentication**: Token-based via `BOT_RELOAD_TOKEN` environment variable
- **Content Type**: JSON for requests and responses
- **CORS**: Enabled for web dashboard access

## 🔐 Authentication

Include the reload token in the `Authorization` header:

```
Authorization: Bearer <BOT_RELOAD_TOKEN>
```

**Example:**
```bash
curl -H "Authorization: Bearer your-token-here" \
     http://localhost:8080/api/strategies
```

## 📊 Strategies API

### List Strategies

**GET** `/api/strategies`

Retrieve all trading strategies with their current status and performance metrics.

**Response:**
```json
{
  "strategies": [
    {
      "id": 1,
      "name": "RSI DCA Strategy",
      "description": "Dollar-cost averaging with RSI signals",
      "algorithm_name": "rsi_dca",
      "enabled": true,
      "cron_expression": "*/5 * * * *",
      "quote_amount": 100.0,
      "max_concurrent_orders": 3,
      "total_orders": 45,
      "successful_orders": 42,
      "total_profit": 1250.50,
      "created_at": "2024-01-15T10:30:00Z",
      "updated_at": "2024-01-20T14:22:00Z",
      "last_executed_at": "2024-01-20T14:20:00Z",
      "next_execution_at": "2024-01-20T14:25:00Z"
    }
  ]
}
```

### Get Strategy

**GET** `/api/strategies/{id}`

Retrieve detailed information about a specific strategy.

**Parameters:**
- `id` (path): Strategy ID

**Response:** Single strategy object (same format as list endpoint)

### Create Strategy

**POST** `/api/strategies`

Create a new trading strategy.

**Request Body:**
```json
{
  "name": "New Strategy",
  "description": "Strategy description",
  "algorithm_name": "rsi_dca",
  "enabled": true,
  "cron_expression": "*/10 * * * *",
  "quote_amount": 50.0,
  "max_concurrent_orders": 2
}
```

**Response:** Created strategy object with generated ID

### Update Strategy

**PUT** `/api/strategies/{id}`

Update an existing strategy.

**Parameters:**
- `id` (path): Strategy ID

**Request Body:** Same as create, all fields optional

**Response:** Updated strategy object

### Delete Strategy

**DELETE** `/api/strategies/{id}`

Delete a strategy (only if no active orders).

**Parameters:**
- `id` (path): Strategy ID

**Response:**
```json
{
  "success": true,
  "message": "Strategy deleted successfully"
}
```

## 💰 Orders API

### List Orders

**GET** `/api/orders`

Retrieve orders with optional filtering.

**Query Parameters:**
- `status` (optional): `pending`, `filled`, `cancelled`, `expired`
- `strategy_id` (optional): Filter by strategy
- `limit` (optional): Maximum results (default: 50)
- `offset` (optional): Pagination offset (default: 0)

**Response:**
```json
{
  "orders": [
    {
      "id": 123,
      "external_id": "mexc_order_12345",
      "side": "buy",
      "amount": 0.001,
      "price": 45000.00,
      "fees": 0.045,
      "status": "filled",
      "strategy_id": 1,
      "cycle_id": 67,
      "created_at": "2024-01-20T14:20:00Z",
      "updated_at": "2024-01-20T14:20:05Z",
      "filled_at": "2024-01-20T14:20:05Z"
    }
  ],
  "total": 1,
  "limit": 50,
  "offset": 0
}
```

### Get Order

**GET** `/api/orders/{id}`

Retrieve detailed information about a specific order.

**Parameters:**
- `id` (path): Order ID

**Response:** Single order object

### Cancel Order

**POST** `/api/orders/{id}/cancel`

Cancel a pending order.

**Parameters:**
- `id` (path): Order ID

**Request Body:** Empty

**Response:**
```json
{
  "success": true,
  "message": "Order cancellation requested"
}
```

## 🔄 Cycles API

### List Cycles

**GET** `/api/cycles`

Retrieve trading cycles with optional filtering.

**Query Parameters:**
- `status` (optional): `open`, `closed`, `cancelled`
- `strategy_id` (optional): Filter by strategy
- `limit` (optional): Maximum results (default: 50)

**Response:**
```json
{
  "cycles": [
    {
      "id": 67,
      "strategy_id": 1,
      "buy_order_id": 123,
      "sell_order_id": 124,
      "max_price": 46500.00,
      "target_price": 47250.00,
      "status": "closed",
      "created_at": "2024-01-20T14:20:00Z",
      "updated_at": "2024-01-20T15:45:00Z",
      "closed_at": "2024-01-20T15:45:00Z"
    }
  ]
}
```

### Get Cycle

**GET** `/api/cycles/{id}`

Retrieve detailed information about a specific trading cycle.

**Parameters:**
- `id` (path): Cycle ID

**Response:** Single cycle object with related order details

## 📈 Statistics API

### Performance Metrics

**GET** `/api/stats`

Retrieve overall trading performance metrics.

**Response:**
```json
{
  "total_strategies": 3,
  "active_strategies": 3,
  "total_orders": 156,
  "pending_orders": 2,
  "filled_orders": 148,
  "cancelled_orders": 6,
  "open_cycles": 8,
  "closed_cycles": 73,
  "total_profit": 3456.78,
  "win_rate": 0.87,
  "average_profit_per_cycle": 47.35,
  "largest_win": 234.56,
  "largest_loss": -45.67,
  "total_fees": 123.45
}
```

### Profit Analysis

**GET** `/api/stats/profit`

Detailed profit/loss analysis with time-based breakdowns.

**Query Parameters:**
- `period` (optional): `day`, `week`, `month` (default: `month`)
- `limit` (optional): Number of periods to return

**Response:**
```json
{
  "period": "month",
  "data": [
    {
      "period": "2024-01",
      "profit": 1234.56,
      "cycles": 45,
      "win_rate": 0.89,
      "avg_profit": 27.43
    },
    {
      "period": "2024-02",
      "profit": -234.12,
      "cycles": 28,
      "win_rate": 0.75,
      "avg_profit": -8.36
    }
  ]
}
```

## 📊 Market Data API

### Current Price

**GET** `/api/market/price`

Get current market price for the configured trading pair.

**Response:**
```json
{
  "exchange": "mexc",
  "pair": "BTC/USDC",
  "price": 45123.45,
  "timestamp": "2024-01-20T16:30:00Z"
}
```

### Candles

**GET** `/api/market/candles`

Retrieve historical candle data.

**Query Parameters:**
- `timeframe` (optional): `1m`, `5m`, `1h`, `1d` (default: `5m`)
- `limit` (optional): Number of candles (default: 100, max: 500)

**Response:**
```json
{
  "exchange": "mexc",
  "pair": "BTC/USDC",
  "timeframe": "5m",
  "candles": [
    {
      "timestamp": 1705766400,
      "open": 45000.00,
      "high": 45200.00,
      "low": 44950.00,
      "close": 45150.00,
      "volume": 123.45
    }
  ]
}
```

## 🔧 System API

### Health Check

**GET** `/api/health`

Check system health and connectivity.

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2024-01-20T16:30:00Z",
  "version": "1.0.0",
  "database": {
    "status": "connected",
    "migrations_applied": 7
  },
  "exchange": {
    "status": "connected",
    "latency_ms": 45
  }
}
```

### Reload Configuration

**POST** `/api/reload`

Reload configuration and restart strategies (requires authentication).

**Response:**
```json
{
  "success": true,
  "message": "Configuration reloaded successfully",
  "restarted_strategies": 3
}
```

## 📋 Error Handling

All API endpoints follow consistent error response format:

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Invalid strategy parameters",
    "details": {
      "field": "cron_expression",
      "reason": "Invalid cron format"
    }
  }
}
```

### Common Error Codes

- `VALIDATION_ERROR`: Invalid request parameters
- `NOT_FOUND`: Resource not found
- `UNAUTHORIZED`: Missing or invalid authentication
- `FORBIDDEN`: Insufficient permissions
- `INTERNAL_ERROR`: Server-side error
- `EXCHANGE_ERROR`: Exchange API connectivity issues

## 🔗 Web Dashboard

The web interface provides a user-friendly dashboard at the root URL (`/`):

- **Strategy Management**: View, create, edit, and delete strategies
- **Real-time Monitoring**: Live order and cycle tracking
- **Performance Charts**: Profit/loss visualization
- **System Status**: Health checks and connectivity status
- **Log Viewer**: Recent system activity

## 📝 Usage Examples

### Monitor Active Orders

```bash
# Get all pending orders
curl -H "Authorization: Bearer $TOKEN" \
     "http://localhost:8080/api/orders?status=pending"
```

### Create New Strategy

```bash
curl -X POST \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{
       "name": "BTC RSI Strategy",
       "algorithm_name": "rsi_dca",
       "cron_expression": "*/5 * * * *",
       "quote_amount": 100
     }' \
     http://localhost:8080/api/strategies
```

### Check Performance

```bash
# Get overall statistics
curl -H "Authorization: Bearer $TOKEN" \
     http://localhost:8080/api/stats

# Get monthly profit breakdown
curl -H "Authorization: Bearer $TOKEN" \
     "http://localhost:8080/api/stats/profit?period=month"
```

This API provides comprehensive access to all bot functionality for monitoring, management, and integration with external trading systems.