# lars-script-runner

Tiny command line tool that will make sure a list of scripts or other commands are always running.

If one of the commands exits, with or without errors, this tool will wait 1 second before attempting to restart it.

## How to build and test:

    git clone https://github.com/lab1702/lars-script-runner.git
    cd lars-script-runner
    go build
    ./lars-script-runner

## How to configure:

Edit the **commands.txt** file to contain all the commands you want to have running at all times, putting one command on each line.

## To use a command list of a different name and/or location:

    ./lars-script-runner -f /path/to/commands.txt

## Compatibility:

This was developed on Windows Server 2022 and Ubuntu 22.04 LTS and the example will run as is as on Windows and on Linux if PowerShell is installed.

The **command.txt** file can contain anything, so you can launch bash scripts, binaries etc.
