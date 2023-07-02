//  Copyright (C) 2020 Maker Ecosystem Growth Holdings, INC.
//
//  This program is free software: you can redistribute it and/or modify
//  it under the terms of the GNU Affero General Public License as
//  published by the Free Software Foundation, either version 3 of the
//  License, or (at your option) any later version.
//
//  This program is distributed in the hope that it will be useful,
//  but WITHOUT ANY WARRANTY; without even the implied warranty of
//  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//  GNU Affero General Public License for more details.
//
//  You should have received a copy of the GNU Affero General Public License
//  along with this program.  If not, see <http://www.gnu.org/licenses/>.

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/chronicleprotocol/oracle-suite/pkg/log"
	"github.com/chronicleprotocol/oracle-suite/pkg/price/provider"
	"github.com/chronicleprotocol/oracle-suite/pkg/price/provider/marshal"
)

// HTTPAgentConfig is the configuration for Lair.
type HTTPAgentConfig struct {
	PriceProvider provider.Provider
	PriceHook     provider.PriceHook
	Marshaller    marshal.Marshaller
	Logger        log.Logger
	// Address is used for the rpc.Listener function.
	Address string
}

// HTTPAgent returns the services that are configured from the Config struct.
type HTTPAgent struct {
	ctx    context.Context
	waitCh chan error

	address       string
	server        *http.Server
	priceProvider provider.Provider
	priceHook     provider.PriceHook
	marshaller    marshal.Marshaller
	log           log.Logger
}

type priceRequest struct {
	Pairs []provider.Pair
}

func NewHTTPAgent(cfg HTTPAgentConfig) *HTTPAgent {
	return &HTTPAgent{
		waitCh:        make(chan error),
		address:       cfg.Address,
		priceProvider: cfg.PriceProvider,
		priceHook:     cfg.PriceHook,
		marshaller:    cfg.Marshaller,
		log:           cfg.Logger,
		server:        &http.Server{Addr: cfg.Address},
	}
}

// Start implements the supervisor.Service interface.
func (s *HTTPAgent) Start(ctx context.Context) error {
	if s.ctx != nil {
		return errors.New("service can be started only once")
	}
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	s.log.Debug("Starting")
	s.ctx = ctx

	err := s.initServer()
	if err != nil {
		return err
	}

	go func() {
		s.log.Debug("Starting HTTP server")
		err := s.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.WithError(err).Error("HTTP server crashed")
		}
	}()
	go s.contextCancelHandler()
	return nil
}

// Wait implements the supervisor.Service interface.
func (s *HTTPAgent) Wait() <-chan error {
	return s.waitCh
}

func (s *HTTPAgent) initServer() error {

	s.log.Infof("initializing HTTP server on %s", s.address)

	http.HandleFunc("/", s.handlePrices)
	http.HandleFunc("/prices", s.handlePrices)

	return nil
}

func (s *HTTPAgent) contextCancelHandler() {
	defer func() { close(s.waitCh) }()
	defer s.log.Debug("Stopped")
	<-s.ctx.Done()
	s.waitCh <- s.server.Close()
}

func (s *HTTPAgent) handlePrices(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-Type") != "application/json" {
		msg := "Content-Type header is not application/json"
		http.Error(w, msg, http.StatusUnsupportedMediaType)
		return
	}

	var p priceRequest
	err := json.NewDecoder(r.Body).Decode(&p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if p.Pairs == nil || len(p.Pairs) == 0 {
		_, _ = io.WriteString(w, "{}")
		return
	}

	prices, err := s.priceProvider.Prices(p.Pairs...)
	if err != nil {
		s.log.Errorf("failed to get prices: %v", err)
		_, _ = io.WriteString(w, `{"error":"failed to get prices"}`)
		return
	}
	err = s.priceHook.Check(prices)
	if err != nil {
		s.log.Errorf("failed to check prices: %v", err)
		_, _ = io.WriteString(w, `{"error":"failed to check prices"}`)
		return
	}

	for _, p := range prices {
		if mErr := s.marshaller.Write(w, p); mErr != nil {
			_ = s.marshaller.Write(w, mErr)
		}
	}
	err = s.marshaller.Flush()
	if err != nil {
		s.log.Errorf("failed to marshal response: %v", err)
		_, _ = io.WriteString(w, `{"error":"failed to marshal json"}`)
		return
	}
	//_, _ = io.WriteString(w, string(b))
}
