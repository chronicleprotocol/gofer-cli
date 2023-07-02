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

package main

import (
	"context"
	"gofer-cli/pkg/agent"
	"os"
	"os/signal"

	"github.com/chronicleprotocol/oracle-suite/pkg/price/provider/marshal"
	"github.com/spf13/cobra"

	"github.com/chronicleprotocol/oracle-suite/pkg/config"
)

func NewAgentCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "agent",
		Args:  cobra.NoArgs,
		Short: "Start an RPC server",
		Long:  `Start an RPC server.`,
		RunE: func(_ *cobra.Command, args []string) error {
			if err := config.LoadFiles(&opts.Config, opts.ConfigFilePath); err != nil {
				return err
			}
			ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt)
			services, err := opts.Config.ClientServices(ctx, opts.Logger(), true, marshal.JSON)
			if err != nil {
				return err
			}
			if err = services.Start(ctx); err != nil {
				return err
			}
			cfg := agent.HTTPAgentConfig{
				PriceProvider: services.PriceProvider,
				PriceHook:     services.PriceHook,
				Marshaller:    services.Marshaller,
				Logger:        services.Logger,
				Address:       opts.Config.Gofer.RPCListenAddr,
			}
			httpAgent := agent.NewHTTPAgent(cfg)
			err = httpAgent.Start(ctx)
			if err != nil {
				return err
			}
			<-services.Wait()
			return <-httpAgent.Wait()
		},
	}
}
