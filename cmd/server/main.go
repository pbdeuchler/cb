package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	"github.com/pbdeuchler/claude-bot/internal/config"
	"github.com/pbdeuchler/claude-bot/internal/db"
	"github.com/pbdeuchler/claude-bot/internal/session"
	slackHandler "github.com/pbdeuchler/claude-bot/internal/slack"
)

type Server struct {
	config       *config.Config
	db           *db.DB
	sessionMgr   *session.Manager
	slackClient  *slack.Client
	eventHandler *slackHandler.EventHandler
	server       *http.Server
}

func main() {
	log.Println("Starting Claude Bot service...")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize database
	database, err := db.NewDB(cfg.Database.Path)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Initialize session manager
	sessionMgr := session.NewManager(database, cfg)

	// Initialize Slack client
	slackClient := slack.New(cfg.Slack.BotToken)

	// Get bot user ID
	authResp, err := slackClient.AuthTest()
	if err != nil {
		log.Fatalf("Failed to authenticate with Slack: %v", err)
	}
	botUserID := authResp.UserID

	// Initialize event handler
	eventHandler := slackHandler.NewEventHandler(slackClient, sessionMgr, botUserID, cfg.Slack.SigningSecret)

	// Create server
	server := &Server{
		config:       cfg,
		db:           database,
		sessionMgr:   sessionMgr,
		slackClient:  slackClient,
		eventHandler: eventHandler,
	}

	// Start idle session monitor
	go sessionMgr.StartIdleSessionMonitor(context.Background())

	// Start server
	if err := server.Start(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func (s *Server) Start() error {
	// Create HTTP router
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", s.healthCheckHandler)

	// Slack events endpoint
	mux.HandleFunc("/slack/events", s.slackEventsHandler)

	// Metrics endpoint (if enabled)
	if s.config.Monitoring.MetricsEnabled {
		mux.Handle("/metrics", promhttp.Handler())
	}

	// Create HTTP server
	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Server.Port),
		Handler:      mux,
		ReadTimeout:  time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.config.Server.WriteTimeout) * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Server starting on port %d", s.config.Server.Port)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// End all active sessions
	if err := s.sessionMgr.EndAllActiveSessions(ctx); err != nil {
		log.Printf("Error ending sessions during shutdown: %v", err)
	}

	// Shutdown HTTP server
	return s.server.Shutdown(ctx)
}

func (s *Server) healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	checks := map[string]bool{
		"database": s.checkDatabase(),
		"slack":    s.checkSlackConnection(),
	}

	healthy := true
	for _, ok := range checks {
		if !ok {
			healthy = false
			break
		}
	}

	status := http.StatusOK
	if !healthy {
		status = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"healthy": healthy,
		"checks":  checks,
		"timestamp": time.Now().Unix(),
	})
}

func (s *Server) checkDatabase() bool {
	return s.db.Ping() == nil
}

func (s *Server) checkSlackConnection() bool {
	_, err := s.slackClient.AuthTest()
	return err == nil
}

func (s *Server) slackEventsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Failed to read request body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Parse event
	event, err := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionNoVerifyToken())
	if err != nil {
		log.Printf("Failed to parse Slack event: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Handle URL verification challenge
	if event.Type == slackevents.URLVerification {
		var challenge *slackevents.ChallengeResponse
		if err := json.Unmarshal(body, &challenge); err != nil {
			log.Printf("Failed to unmarshal challenge: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(challenge.Challenge))
		return
	}

	// Handle callback events
	if event.Type == slackevents.CallbackEvent {
		ctx := context.Background()
		
		switch evData := event.InnerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			if err := s.eventHandler.HandleAppMention(ctx, evData); err != nil {
				log.Printf("Failed to handle app mention: %v", err)
			}
		case *slackevents.MessageEvent:
			if err := s.eventHandler.HandleMessage(ctx, evData); err != nil {
				log.Printf("Failed to handle message: %v", err)
			}
		default:
			log.Printf("Unhandled event type: %T", evData)
		}
	}

	w.WriteHeader(http.StatusOK)
}