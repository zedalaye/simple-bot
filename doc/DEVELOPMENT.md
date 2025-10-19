# Development Guide

This guide covers the development setup, coding standards, testing practices, and contribution guidelines for the Simple Trading Bot.

## 🚀 Getting Started

### Prerequisites

- **Go 1.21+**: [Download here](https://go.dev/dl/)
- **Git**: Version control
- **SQLite 3**: Database engine (usually pre-installed)
- **Make**: Build automation (usually pre-installed)
- **Docker** (optional): For containerized development

### Alternative: Mise-en-Place

[Mise-en-Place](https://mise.jdx.dev/) provides automatic tool version management:

```bash
# Install mise
curl https://mise.jdx.dev/install.sh | sh

# Install tools automatically
mise install
```

### Clone and Setup

```bash
# Clone repository
git clone <repository-url>
cd simple-trading-bot

# Install dependencies
go mod download

# Build all binaries
make

# Run tests
make test

# Verify installation
./bin/bot --help
```

## 🏗️ Project Structure

```
simple-trading-bot/
├── cmd/                    # Executable entry points
│   ├── bot/               # Main trading bot
│   ├── web/               # Web interface
│   ├── admin/             # Admin tools
│   └── test/              # Testing utilities
│
├── internal/              # Private application code
│   ├── bot/              # Bot engine core
│   ├── scheduler/        # Strategy scheduling
│   ├── algorithms/       # Trading algorithms
│   ├── market/           # Market data handling
│   ├── database/         # Data persistence
│   ├── web/              # HTTP handlers
│   └── core/             # Shared utilities
│
├── storage/              # Configuration & data (gitignored)
│   ├── mexc/            # MEXC exchange config
│   └── hl/              # Hyperliquid config
│
├── doc/                  # Documentation
├── docker/               # Docker files
├── scripts/              # Build/deployment scripts
├── go.mod               # Go module definition
└── Makefile             # Build automation
```

### Key Design Principles

- **Hexagonal Architecture**: Clear separation between business logic and external dependencies
- **Dependency Injection**: Interfaces for testability and flexibility
- **Single Responsibility**: Each package has a focused purpose
- **Composition over Inheritance**: Struct embedding and interface composition

## 💻 Development Workflow

### 1. Choose Your Task

**Available Development Areas:**
- **New Trading Algorithm**: Implement new strategy in `internal/algorithms/`
- **Exchange Integration**: Add support for new exchanges in `internal/exchange/`
- **Web Features**: Extend API or UI in `internal/web/`
- **Database Changes**: Add migrations in `internal/core/database/migrations/`
- **Testing Tools**: Enhance testing utilities in `cmd/test/`

### 2. Create Feature Branch

```bash
# Create and switch to feature branch
git checkout -b feature/new-trading-algorithm

# Make your changes...
```

### 3. Implement Changes

**Coding Standards:**
- Follow Go conventions and effective Go practices
- Use `gofmt` for consistent formatting
- Add comprehensive documentation
- Write tests for new functionality

**Example: Adding a New Algorithm**

```go
// internal/algorithms/my_algorithm.go
package algorithms

type MyAlgorithm struct{}

func (a *MyAlgorithm) Name() string { return "my_algorithm" }

func (a *MyAlgorithm) ShouldBuy(ctx TradingContext, strategy database.Strategy) (BuySignal, error) {
    // Implementation here
    return BuySignal{ShouldBuy: true, Amount: 100, LimitPrice: 50000}, nil
}

func (a *MyAlgorithm) ShouldSell(ctx TradingContext, cycle database.Cycle, strategy database.Strategy) (SellSignal, error) {
    // Implementation here
    return SellSignal{ShouldSell: false, Reason: "Not yet"}, nil
}

// Register in algorithm registry
// internal/algorithms/algorithm.go
func NewAlgorithmRegistry() *AlgorithmRegistry {
    registry := &AlgorithmRegistry{algorithms: make(map[string]Algorithm)}
    registry.Register(&RSI_DCA{})
    registry.Register(&MACD_Cross{})
    registry.Register(&MyAlgorithm{})  // Add your new algorithm
    return registry
}
```

### 4. Write Tests

**Testing Strategy:**
- Unit tests for individual functions
- Integration tests for component interaction
- End-to-end tests for complete workflows

```bash
# Run all tests
make test

# Run specific package tests
go test ./internal/algorithms/...

# Run with coverage
go test -cover ./internal/algorithms/

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

**Example Test:**

```go
// internal/algorithms/my_algorithm_test.go
func TestMyAlgorithm_ShouldBuy(t *testing.T) {
    algorithm := &MyAlgorithm{}
    ctx := TradingContext{
        CurrentPrice: 50000,
        Balance: map[string]Balance{
            "USDC": {Free: 1000},
        },
    }
    strategy := database.Strategy{
        QuoteAmount: 100,
    }

    signal, err := algorithm.ShouldBuy(ctx, strategy)

    assert.NoError(t, err)
    assert.True(t, signal.ShouldBuy)
    assert.Equal(t, 100.0, signal.Amount)
}
```

### 5. Update Documentation

- Add algorithm documentation in code comments
- Update relevant docs in `doc/` directory
- Add configuration examples if needed

### 6. Submit Pull Request

```bash
# Ensure all tests pass
make test

# Format code
gofmt -w .

# Commit changes
git add .
git commit -m "feat: add new trading algorithm

- Implement MyAlgorithm with buy/sell logic
- Add comprehensive tests
- Update algorithm registry"

# Push and create PR
git push origin feature/new-trading-algorithm
```

## 🧪 Testing Practices

### Test Categories

**1. Unit Tests**
- Test individual functions and methods
- Mock external dependencies
- Focus on business logic

**2. Integration Tests**
- Test component interactions
- Use real database for some tests
- Validate data flow between layers

**3. End-to-End Tests**
- Test complete user workflows
- Use test exchanges or paper trading
- Validate system behavior

### Testing Tools

```bash
# Run tests with race detection
go test -race ./...

# Run benchmarks
go test -bench=. ./internal/algorithms/

# Run tests in verbose mode
go test -v ./...

# Generate test coverage badge
go test -coverprofile=coverage.out ./...
```

### Test Database

For integration tests, use a separate test database:

```go
// Create test database
testDB, err := database.NewDB(":memory:")
require.NoError(t, err)

// Run migrations
err = testDB.RunMigrations()
require.NoError(t, err)

// Use testDB for testing
```

## 🔧 Development Tools

### Code Quality

```bash
# Format code
gofmt -w .

# Vet code for suspicious constructs
go vet ./...

# Run linter (install golangci-lint first)
golangci-lint run

# Check for security issues
gosec ./...
```

### Database Tools

```bash
# View database schema
sqlite3 storage/mexc/db/bot.db ".schema"

# Query data interactively
sqlite3 storage/mexc/db/bot.db

# Backup database
sqlite3 storage/mexc/db/bot.db ".backup backup.db"
```

### Debugging

```bash
# Enable debug logging
export LOG_LEVEL=debug

# Run with profiling
go run -cpuprofile=cpu.prof ./cmd/bot

# Analyze profile
go tool pprof cpu.prof
```

## 🚀 Deployment & Release

### Local Development

```bash
# Run bot with hot reload
make dev

# Run web interface
make web-dev

# Run all services with Docker
make docker-dev
```

### Production Deployment

```bash
# Build optimized binaries
make build-release

# Create deployment package
make package

# Deploy with Docker
make deploy
```

### Release Process

1. **Version Bump**: Update version in relevant files
2. **Changelog**: Document changes in `CHANGELOG.md`
3. **Tag Release**: Create git tag
4. **Build Artifacts**: Generate release binaries
5. **Docker Images**: Build and push images
6. **Documentation**: Update deployment docs

## 🤝 Contributing Guidelines

### Code of Conduct
- Be respectful and inclusive
- Focus on constructive feedback
- Help newcomers learn

### Pull Request Process
1. **Fork** the repository
2. **Create** a feature branch
3. **Implement** your changes with tests
4. **Update** documentation as needed
5. **Submit** pull request with clear description
6. **Address** review feedback
7. **Merge** after approval

### Commit Message Format
```
type(scope): description

[optional body]

[optional footer]
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation
- `style`: Code style changes
- `refactor`: Code refactoring
- `test`: Testing
- `chore`: Maintenance

**Examples:**
```
feat: add RSI divergence algorithm
fix: correct order status update race condition
docs: update API documentation for v2 endpoints
```

### Issue Reporting
- Use issue templates when available
- Provide clear reproduction steps
- Include relevant logs and configuration
- Specify environment details (Go version, OS, etc.)

## 📚 Learning Resources

### Go Development
- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)

### Trading & Finance
- [Technical Analysis Concepts](https://www.investopedia.com/terms/t/technicalanalysis.asp)
- [Algorithmic Trading Basics](https://www.quantconnect.com/docs/algorithm-reference/overview)
- [Risk Management Principles](https://www.investopedia.com/articles/trading/09/risk-management.asp)

### Project-Specific
- [ARCHITECTURE.md](ARCHITECTURE.md) - System design overview
- [DATABASE.md](DATABASE.md) - Data model and migrations
- [API.md](API.md) - Web API reference

This development guide provides the foundation for contributing effectively to the Simple Trading Bot project while maintaining code quality and following established practices.