# cmd
--
    import "github.com/msample/cutkmz/cmd"

cutkmz subcommands

Other than root.go, each of these go files is a cutkmz subcommand implementation

## Usage

```go
var RootCmd = &cobra.Command{
	Use:   "cutkmz",
	Short: "Creates .kmz map tiles for a Garmin GPS from a JPG",
	Long:  `see help on the kmz and rename subcommands`,
}
```
This represents the base command when called without any subcommands

#### func  Execute

```go
func Execute()
```
Execute adds all child commands to the root command sets flags appropriately.
This is called by main.main(). It only needs to happen once to the rootCmd.
