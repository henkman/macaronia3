package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	twitch "github.com/gempir/go-twitch-irc"
	"github.com/henkman/steamquery"
)

type Alias struct {
	Command Command
	Format  string
}

type AliasUnresolved struct {
	Command string
	Format  string
}

type Command = func(client *twitch.Client, user twitch.User, channel string, text string)

func getRegexFromCommandsAndAliases(cmdChar string, cmds map[string]Command, alias map[string]Alias) (*regexp.Regexp, error) {
	s := make([]string, 0, len(cmds)+len(alias))
	for c, _ := range cmds {
		s = append(s, c)
	}
	for a, _ := range alias {
		s = append(s, a)
	}
	return regexp.Compile("^" + cmdChar + "(" + strings.Join(s, "|") + ")[ $](.*?)$")
}

func main() {
	var config struct {
		Channels    []string `json:"channels"`
		Username    string   `json:"username"`
		Oauth       string   `json:"oauth"`
		CommandChar string   `json:"commandchar"`
	}
	{
		fd, err := os.Open("config.json")
		if err != nil {
			log.Panic(err)
		}
		err = json.NewDecoder(fd).Decode(&config)
		fd.Close()
		if err != nil {
			log.Panic(err)
		}
	}
	log.Printf("%+v\n", config)

	aliases := map[string]Alias{}

	var commands = map[string]Command{
		"alias": func(client *twitch.Client, user twitch.User, channel string, text string) {
			// alias set name command text %s
			// alias rm name
			client.Say(channel, "NOT IMPLEMENTED")
		},
		"ison": func(client *twitch.Client, user twitch.User, channel string, text string) {
			// ison server1;serverN filter1;filterN
			client.Say(channel, "NOT IMPLEMENTED")
		},
		"players": func(client *twitch.Client, user twitch.User, channel string, text string) {
			// players serveraddress
			players, _, err := steamquery.QueryPlayersString(text)
			if err != nil {
				log.Println(err)
				return
			}
			var sb strings.Builder
			for _, p := range players {
				sb.WriteString(fmt.Sprintf("%s in game for %v\n", p.Name, p.Duration))
			}
			client.Say(channel, sb.String())
		},
		"info": func(client *twitch.Client, user twitch.User, channel string, text string) {
			// info serveraddress
			info, _, err := steamquery.QueryInfoString(text)
			if err != nil {
				log.Println(err)
				return
			}
			client.Say(channel, fmt.Sprintf("%d/%d on %s playing %s",
				info.Players, info.MaxPlayers, info.Name, info.Map))
		},
	}

	{ // alias restore
		fd, err := os.Open("aliases.json")
		if err != nil {
			log.Panic(err)
		}
		aus := map[string]AliasUnresolved{}
		err = json.NewDecoder(fd).Decode(&aus)
		fd.Close()
		if err != nil {
			log.Panic(err)
		}
		for k, au := range aus {
			cmd, ok := commands[au.Command]
			if !ok {
				log.Fatal("could not resolve alias", au.Command)
			}
			aliases[k] = Alias{
				Command: cmd,
				Format:  au.Format,
			}
		}
		fmt.Println(aliases)
	}

	reCommands, err := getRegexFromCommandsAndAliases(config.CommandChar, commands, aliases)
	if err != nil {
		log.Panic(err)
	}

	client := twitch.NewClient(config.Username, config.Oauth)
	client.OnPrivateMessage(func(message twitch.PrivateMessage) {
		m := reCommands.FindStringSubmatch(message.Message)
		if m == nil {
			return
		}
		if cmd, ok := commands[m[1]]; ok {
			cmd(client, message.User, message.Channel, m[2])
		}
		if alias, ok := aliases[m[1]]; ok {
			text := fmt.Sprintf(alias.Format, m[2])
			alias.Command(client, message.User, message.Channel, text)
		}
	})
	client.OnConnect(func() {
		for _, channel := range config.Channels {
			log.Println("joining channel", channel)
			client.Join(channel)
		}
	})
	log.Println("connecting")
	if err := client.Connect(); err != nil {
		log.Panic(err)
	}
}
