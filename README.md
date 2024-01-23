# lars-script-runner

Tiny command line tool that will make sure a list of scripts or other commands are always running.

If one of the commands exits, with or without errors, this tool will make sure each command is restarted no more often than once per second.

The functionality can most likely be replicated with a small shell or powershell script,
but I wanted to learn a bit more about [Go](https://go.dev/) and see if it was possible to do something lower level like this with
no platform specific code at all.

## Installation

    go install github.com/lab1702/lars-script-runner@latest

## Downloading source and running the example:

    git clone https://github.com/lab1702/lars-script-runner.git
    cd lars-script-runner
    go build
    ./lars-script-runner

If all works, to install do:

    go install

## How to configure what commands to keep alive:

Edit the **[commands.txt](commands.txt)** file to contain all the commands you want to have running at all times, putting one command on each line.

## To use a command list of a different name and/or location:

    ./lars-script-runner -f /path/to/commands.txt

## Compatibility:

This was developed on Windows Server 2022 and Ubuntu 22.04 LTS and the example is tested to run as is as on Windows and on Linux if PowerShell is installed.

The **[commands.txt](commands.txt)** file can contain anything, so you can launch bash scripts, binaries etc.
