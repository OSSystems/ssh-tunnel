package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	shellwords "github.com/mattn/go-shellwords"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func main() {
	var rootCmd = &cobra.Command{
		Use: "ssh-tunnel",
		Run: func(cmd *cobra.Command, args []string) {
		},
	}

	viper.AutomaticEnv()

	deviceID := getDeviceID(viper.GetString("DEVICE_ID"))
	mqttServer := viper.GetString("MQTT_SERVER")
	sshServer := viper.GetString("SSH_SERVER")
	sshPort := viper.GetString("SSH_PORT")
	privateKey := viper.GetString("PRIVATE_KEY")

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}

	helpCalled, err := rootCmd.Flags().GetBool("help")
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	if helpCalled {
		os.Exit(1)
	}

	cmds := make(map[int]*exec.Cmd)

	fmt.Printf("device-id=%s\n", deviceID)

	opts := mqtt.NewClientOptions().AddBroker(fmt.Sprintf("tcp://%s", mqttServer))
	opts.SetAutoReconnect(true)
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		connect(client)
	})
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		if token := client.Subscribe(fmt.Sprintf("connect/%s", deviceID), 0, func(client mqtt.Client, msg mqtt.Message) {
			go func() {
				port, _ := strconv.Atoi(string(msg.Payload()))

				fmt.Printf("reverse port=%d\n", port)

				args := []string{
					"ssh",
					"-i", privateKey,
					"-o", "StrictHostKeyChecking=no",
					"-nNT",
					"-p", sshPort,
					"-R", fmt.Sprintf("%d:localhost:22", port),
					fmt.Sprintf("ssh@%s", sshServer),
				}

				cmd := exec.Command(args[0], args[1:]...)
				_ = cmd.Start()

				cmds[port] = cmd
			}()
		}); token.Wait() && token.Error() != nil {
			log.Fatal(token.Error())
		}

		if token := client.Subscribe(fmt.Sprintf("disconnect/%s", deviceID), 0, func(client mqtt.Client, msg mqtt.Message) {
			port, _ := strconv.Atoi(string(msg.Payload()))

			if cmd, ok := cmds[port]; ok {
				cmd.Process.Kill()
				cmd.Wait()
				delete(cmds, port)
			}
		}); token.Wait() && token.Error() != nil {
			log.Fatal(token.Error())
		}
	})

	client := mqtt.NewClient(opts)

	connect(client)

	select {}
}

func getDeviceID(deviceID string) string {
	parts := strings.Split(deviceID, ":")
	if len(parts) < 2 {
		return deviceID
	}

	switch parts[0] {
	case "value":
		return strings.Join(parts[1:], ":")
	case "exec":
		args, err := shellwords.Parse(strings.Join(parts[1:], ":"))
		if err != nil {
			log.Fatal(err)
		}

		var out bytes.Buffer

		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			log.Fatal(err)
		}

		return strings.TrimSpace(out.String())
	}

	return deviceID
}

func connect(client mqtt.Client) {
	for {
		if token := client.Connect(); token.Wait() && token.Error() != nil {
			time.Sleep(time.Second)
			continue
		}

		break
	}
}
