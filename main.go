package main

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	_ "github.com/lib/pq"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	echologrus "github.com/davrux/echo-logrus/v4"
	"github.com/getAlby/nostr-wallet-connect/migrations"
	"github.com/glebarez/sqlite"
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"github.com/labstack/echo/v4"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	log "github.com/sirupsen/logrus"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/jackc/pgx/v5/stdlib"
	sqltrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/database/sql"
	gormtrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/gorm.io/gorm.v1"
)

func main() {

	// Load config from environment variables / .env file
	envFile := ".env"
	if os.Getenv("NWC_ENV_FILE") != "" {
		envFile = os.Getenv("NWC_ENV_FILE")
	}
	godotenv.Load(envFile)
	cfg := &Config{}
	err := envconfig.Process("", cfg)
	if err != nil {
		log.Fatalf("Error loading environment variables: %v", err)
	}
	// Load from secrets if available.
	if _, err := os.Stat(cfg.UmaVaspJwtPubKeySecretFile); err == nil {
		pubkeyBytes, err := os.ReadFile(cfg.UmaVaspJwtPubKeySecretFile)
		if err != nil {
			log.Fatalf("Error reading UMA VASP JWT public key from file: %v", err)
		}
		cfg.UmaVaspJwtPubKey = string(pubkeyBytes)
	}
	if _, err := os.Stat(cfg.CookieSecretFile); err == nil {
		cookieSecretBytes, err := os.ReadFile(cfg.CookieSecretFile)
		if err != nil {
			log.Fatalf("Error reading cookie secret from file: %v", err)
		}
		cfg.CookieSecret = string(cookieSecretBytes)
	}
	if _, err := os.Stat(cfg.NostrSecretKeyFile); err == nil {
		nostrPrivKey, err := os.ReadFile(cfg.NostrSecretKeyFile)
		if err != nil {
			log.Fatalf("Error reading Nostr private key from file: %v", err)
		}
		cfg.NostrSecretKey = string(nostrPrivKey)
	}

	var db *gorm.DB
	var sqlDb *sql.DB
	if strings.HasPrefix(cfg.DatabaseUri, "postgres://") || strings.HasPrefix(cfg.DatabaseUri, "postgresql://") || strings.HasPrefix(cfg.DatabaseUri, "unix://") {
		if os.Getenv("DATADOG_AGENT_URL") != "" {
			sqltrace.Register("pgx", &stdlib.Driver{}, sqltrace.WithServiceName("nostr-wallet-connect"))
			sqlDb, err = sqltrace.Open("pgx", cfg.DatabaseUri)
			if err != nil {
				log.Fatalf("Failed to open DB %v", err)
			}
			db, err = gormtrace.Open(postgres.New(postgres.Config{Conn: sqlDb}), &gorm.Config{})
			if err != nil {
				log.Fatalf("Failed to open DB %v", err)
			}
		} else {
			if cfg.UseRdsIamAuth {
				log.Infof("Using RDS IAM auth for %s", cfg.DatabaseUri)
				cfg.DatabaseEndpoint = "dev-uma-dogfood.czgjn8lg0uxg.us-west-2.rds.amazonaws.com:5432"
				cfg.DatabaseUser = "uda"
				cfg.DatabaseRegion = "us-west-2"
				log.Infof("Endpoint: %s", cfg.DatabaseEndpoint)
				log.Infof("Region: %s", cfg.DatabaseRegion)
				authToken, err := generateAuthToken(cfg.DatabaseRegion, cfg.DatabaseEndpoint, cfg.DatabaseUser)
				if err != nil {
					log.Fatalf("Failed to generate auth token: %v", err)
				}
				// Strip the port off the endpoint:
				dbHost := cfg.DatabaseEndpoint
				dbPort := 5432
				if strings.Contains(cfg.DatabaseEndpoint, ":") {
					dbHost = strings.Split(cfg.DatabaseEndpoint, ":")[0]
					dbPort, err = strconv.Atoi(strings.Split(cfg.DatabaseEndpoint, ":")[1])
					if err != nil {
						log.Fatalf("Failed to parse port from endpoint: %v", err)
					}
				}

				cfg.DatabaseUri = fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=verify-full sslrootcert=/etc/uma-dogfood/rds-ca.pem",
					dbHost, dbPort, cfg.DatabaseUser, authToken, "nwc",
				)
				log.Infof("now it's - %s", cfg.DatabaseUri)
			} else if cfg.DatabasePassword != "" {
				cfg.DatabaseUri = fmt.Sprintf("user=%s password=%s dbname=%s host=%s sslmode=disable", cfg.DatabaseUser, cfg.DatabasePassword, "nwc", cfg.DatabaseUri)
			}
			db, err = gorm.Open(postgres.New(postgres.Config{DriverName: "pgx", DSN: fmt.Sprintf(cfg.DatabaseUri)}), &gorm.Config{})
			if err != nil {
				log.Fatalf("Failed to open DB %v", err)
			}
			sqlDb, err = db.DB()
			if err != nil {
				log.Fatalf("Failed to set DB config: %v", err)
			}
		}
	} else {
		db, err = gorm.Open(sqlite.Open(cfg.DatabaseUri), &gorm.Config{})
		if err != nil {
			log.Fatalf("Failed to open DB %v", err)
		}
		// Override SQLite config to max one connection
		cfg.DatabaseMaxConns = 1
		// Enable foreign keys for sqlite
		db.Exec("PRAGMA foreign_keys=ON;")
		sqlDb, err = db.DB()
		if err != nil {
			log.Fatalf("Failed to set DB config: %v", err)
		}
	}
	sqlDb.SetMaxOpenConns(cfg.DatabaseMaxConns)
	sqlDb.SetMaxIdleConns(cfg.DatabaseMaxIdleConns)
	sqlDb.SetConnMaxLifetime(time.Duration(cfg.DatabaseConnMaxLifetime) * time.Second)

	err = migrations.Migrate(db)
	if err != nil {
		log.Fatalf("Migration failed: %v", err)
	}
	log.Println("Any pending migrations ran successfully")

	if cfg.NostrSecretKey == "" {
		if cfg.LNBackendType == AlbyBackendType {
			//not allowed
			log.Fatal("Nostr private key is required with this backend type.")
		}
		//first look up if we already have the private key in the database
		//else, generate and store private key
		identity := &Identity{}
		err = db.FirstOrInit(identity).Error
		if err != nil {
			log.WithError(err).Fatal("Error retrieving private key from database")
		}
		if identity.Privkey == "" {
			log.Info("No private key found in database, generating & saving.")
			identity.Privkey = nostr.GeneratePrivateKey()
			err = db.Save(identity).Error
			if err != nil {
				log.WithError(err).Fatal("Error saving private key to database")
			}
		}
		cfg.NostrSecretKey = identity.Privkey
	}

	identityPubkey, err := nostr.GetPublicKey(cfg.NostrSecretKey)
	if err != nil {
		log.Fatalf("Error converting nostr privkey to pubkey: %v", err)
	}
	cfg.IdentityPubkey = identityPubkey
	npub, err := nip19.EncodePublicKey(identityPubkey)
	if err != nil {
		log.Fatalf("Error converting nostr privkey to pubkey: %v", err)
	}

	log.Infof("Starting nostr-wallet-connect. npub: %s hex: %s", npub, identityPubkey)
	svc := &Service{
		cfg: cfg,
		db:  db,
	}

	if os.Getenv("DATADOG_AGENT_URL") != "" {
		tracer.Start(tracer.WithService("nostr-wallet-connect"))
		defer tracer.Stop()
	}

	echologrus.Logger = log.New()
	echologrus.Logger.SetFormatter(&log.JSONFormatter{})
	echologrus.Logger.SetOutput(os.Stdout)
	echologrus.Logger.SetLevel(log.InfoLevel)
	svc.Logger = echologrus.Logger

	e := echo.New()
	ctx := context.Background()
	ctx, _ = signal.NotifyContext(ctx, os.Interrupt)
	var wg sync.WaitGroup
	switch cfg.LNBackendType {
	case LNDBackendType:
		lndClient, err := NewLNDService(ctx, svc, e)
		if err != nil {
			svc.Logger.Fatal(err)
		}
		svc.lnClient = lndClient
	case AlbyBackendType:
		oauthService, err := NewAlbyOauthService(svc, e)
		if err != nil {
			svc.Logger.Fatal(err)
		}
		svc.lnClient = oauthService
	case UmaBackendType:
		umaService, err := NewUmaNwcAdapterService(svc, e)
		if err != nil {
			svc.Logger.Fatal(err)
		}
		svc.lnClient = umaService
	case LightsparkBackendType:
		lightsparkService, err := NewLightsparkService(ctx, svc, e)
		if err != nil {
			svc.Logger.Fatal(err)
		}
		svc.lnClient = lightsparkService
	}

	//register shared routes
	svc.RegisterSharedRoutes(e)
	//start Echo server
	wg.Add(1)
	go func() {
		if err := e.Start(fmt.Sprintf(":%v", svc.cfg.Port)); err != nil && err != http.ErrServerClosed {
			e.Logger.Fatal("shutting down the server")
		}
		//handle graceful shutdown
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		e.Shutdown(ctx)
		svc.Logger.Info("Echo server exited")
		wg.Done()
	}()

	//connect to the relay
	svc.Logger.Infof("Connecting to the relay: %s", cfg.Relay)

	relay, err := nostr.RelayConnect(ctx, cfg.Relay, nostr.WithNoticeHandler(svc.noticeHandler))
	if err != nil {
		svc.Logger.Fatal(err)
	}

	//publish event with NIP-47 info
	err = svc.PublishNip47Info(ctx, relay)
	if err != nil {
		svc.Logger.WithError(err).Error("Could not publish NIP47 info")
	}

	//Start infinite loop which will be only broken by canceling ctx (SIGINT)
	//TODO: we can start this loop for multiple relays
	for {
		svc.Logger.Info("Subscribing to events")
		sub, err := relay.Subscribe(ctx, svc.createFilters())
		if err != nil {
			svc.Logger.Fatal(err)
		}
		err = svc.StartSubscription(ctx, sub)
		if err != nil {
			//err being non-nil means that we have an error on the websocket error channel. In this case we just try to reconnect.
			svc.Logger.WithError(err).Error("Got an error from the relay while listening to subscription. Reconnecting...")
			relay, err = nostr.RelayConnect(ctx, cfg.Relay)
			if err != nil {
				svc.Logger.Fatal(err)
			}
			continue
		}
		//err being nil means that the context was canceled and we should exit the program.
		break
	}
	err = relay.Close()
	if err != nil {
		svc.Logger.Error(err)
	}
	svc.Logger.Info("Graceful shutdown completed. Goodbye.")
}

func generateAuthToken(region, endpoint, username string) (string, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return "", err
	}
	return auth.BuildAuthToken(context.TODO(), endpoint, region, username, cfg.Credentials)
	//sess, err := session.NewSession(&aws.Config{
	//	Region: aws.String(region),
	//})
	//if err != nil {
	//	return "", err
	//}
	//
	//authToken, err := rdsutils.BuildAuthToken(
	//	endpoint,
	//	region,
	//	username,
	//	sess.Config.Credentials,
	//)
	//if err != nil {
	//	return "", err
	//}
	//
	//return authToken, nil
}

func (svc *Service) createFilters() nostr.Filters {
	filter := nostr.Filter{
		Tags:  nostr.TagMap{"p": []string{svc.cfg.IdentityPubkey}},
		Kinds: []int{NIP_47_REQUEST_KIND},
	}
	if svc.cfg.ClientPubkey != "" {
		filter.Authors = []string{svc.cfg.ClientPubkey}
	}
	return []nostr.Filter{filter}
}

func (svc *Service) noticeHandler(notice string) {
	svc.Logger.Infof("Received a notice %s", notice)
}
