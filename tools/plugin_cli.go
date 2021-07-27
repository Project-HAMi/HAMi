/*
 * Copyright © 2021 peizhaoyou <peizhaoyou@4paradigm.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
    "context"
    "fmt"
    "os"
    "time"

    "4pd.io/k8s-vgpu/pkg/api"
    "github.com/spf13/cobra"
    "google.golang.org/grpc"
)

var (
    runtimeSocketFlag string
    rootCmd           = &cobra.Command{
        Use:   "device plugin cli",
        Short: "device plugin socket cli",
    }

    getCmd = &cobra.Command{
        Use: "get devices",
        Short: "get devices",
        Args: cobra.ExactArgs(1),
        Run: func(cmd *cobra.Command, args []string) {
            getDevices(args[0])
        },
    }
)

func init() {
    rootCmd.Flags().SortFlags = false
    rootCmd.PersistentFlags().SortFlags = false
    rootCmd.PersistentFlags().StringVar(&runtimeSocketFlag, "socket", "/var/lib/vgpu/vgpu.sock", "device plugin socket")

    rootCmd.AddCommand(getCmd)
}

func getDevices(uid string) {
    ctx := context.Background()
    ctx, cancel := context.WithTimeout(ctx, time.Second * 10)
    defer cancel()
    conn, err := grpc.DialContext(
        ctx,
        fmt.Sprintf("unix://%v", runtimeSocketFlag),
        grpc.WithInsecure(),
        grpc.WithBlock(),
    )
    if err != nil {
        fmt.Printf("connect device plugin error, %v\n", err)
        os.Exit(1)
    }
    client := api.NewVGPURuntimeServiceClient(conn)
    req := api.GetDeviceRequest{ CtrUUID: uid }
    resp, err := client.GetDevice(ctx, &req)
    if err != nil {
        fmt.Printf("get device failed, %v\n", err)
        os.Exit(1)
    }
    fmt.Printf("res:\n%v\n", resp.String())
}

func main() {
    if err := rootCmd.Execute(); err != nil {
        fmt.Print(err)
        os.Exit(1)
    }
}
