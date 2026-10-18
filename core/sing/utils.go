package sing

import (
	"fmt"

	"github.com/InazumaV/V2bX/conf"
	"github.com/sagernet/sing-box/option"
)

func processFallback(c *conf.Options, fallbackForALPN map[string]*option.ServerOptions) error {
	for k, v := range c.SingOptions.FallBackConfigs.FallBackForALPN {
		fallbackPort, err := parsePort(v.ServerPort)
		if err != nil {
			return fmt.Errorf("unable to parse fallbackForALPN server port for alpn %q: %w", k, err)
		}
		fallbackForALPN[k] = &option.ServerOptions{Server: v.Server, ServerPort: fallbackPort}
	}
	return nil
}
