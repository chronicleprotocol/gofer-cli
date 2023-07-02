//  Copyright (C) 2021-2023 Chronicle Labs, Inc.
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

package prices

import (
	"context"
	"errors"

	"github.com/chronicleprotocol/oracle-suite/pkg/log"
	"github.com/chronicleprotocol/oracle-suite/pkg/log/null"
	"github.com/chronicleprotocol/oracle-suite/pkg/price/median"
	"github.com/chronicleprotocol/oracle-suite/pkg/price/provider"
	"github.com/chronicleprotocol/oracle-suite/pkg/util/timeutil"
)

const LoggerTag = "PRICE_CACHE"

// Cache is a service which periodically fetches prices and keeps them in cache.
type Cache struct {
	ctx    context.Context
	waitCh chan error

	interval      *timeutil.Ticker
	priceProvider provider.Provider
	pairs         []provider.Pair
	log           log.Logger
	prices        map[provider.Pair]provider.Price
}

// Config is the configuration for the Cache.
type Config struct {
	// Pairs is a list supported pairs in the format "QUOTE/BASE".
	Pairs []string

	// PriceProvider is a price provider which is used to fetch prices.
	PriceProvider provider.Provider

	// Interval describes how often we should send prices to the network.
	Interval *timeutil.Ticker

	// Logger is a current logger interface used by the Cache.
	Logger log.Logger
}

// New creates a new instance of the Cache.
func New(cfg Config) (*Cache, error) {
	if cfg.PriceProvider == nil {
		return nil, errors.New("price provider must not be nil")
	}
	if cfg.Logger == nil {
		cfg.Logger = null.New()
	}
	pairs, err := provider.NewPairs(cfg.Pairs...)
	if err != nil {
		return nil, err
	}
	g := &Cache{
		waitCh:        make(chan error),
		priceProvider: cfg.PriceProvider,
		interval:      cfg.Interval,
		pairs:         pairs,
		log:           cfg.Logger.WithField("tag", LoggerTag),
	}
	return g, nil
}

// Start implements the supervisor.Service interface.
func (g *Cache) Start(ctx context.Context) error {
	if g.ctx != nil {
		return errors.New("service can be started only once")
	}
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	g.log.Debug("Starting")
	g.ctx = ctx
	g.interval.Start(g.ctx)
	go g.broadcasterRoutine()
	go g.contextCancelHandler()
	return nil
}

// Wait implements the supervisor.Service interface.
func (g *Cache) Wait() <-chan error {
	return g.waitCh
}

// update sends price for single pair to the network. This method uses
// current price from the Provider, so it must be updated beforehand.
func (g *Cache) update(pair provider.Pair) error {
	var err error

	// Create price.
	tick, err := g.priceProvider.Price(pair)
	if err != nil {
		return err
	}
	if tick.Error != "" {
		return errors.New(tick.Error)
	}
	price := &median.Price{Wat: pair.Base + pair.Quote, Age: tick.Time}
	price.SetFloat64Price(tick.Price)

	return err
}

func (g *Cache) broadcasterRoutine() {
	for {
		select {
		case <-g.ctx.Done():
			return
		case <-g.interval.TickCh():
			// Send prices to the network.
			for _, pair := range g.pairs {
				if err := g.update(pair); err != nil {
					g.log.
						WithField("assetPair", pair).
						WithError(err).
						Warn("Unable to update price")
					continue
				}
				g.log.
					WithField("assetPair", pair).
					Info("Price update")
			}
		}
	}
}

func (g *Cache) contextCancelHandler() {
	defer func() { close(g.waitCh) }()
	defer g.log.Debug("Stopped")
	<-g.ctx.Done()
}
