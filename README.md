# lars-script-runner

Tiny command line tool that will make sure a list of scripts or other commands are always running.

## How to build and test:

    git clone https://github.com/lab1702/lars-script-runner.git
    cd lars-script-runner
    go build
    ./lars-script-runner

## How to configure:

Edit the **commands.txt** file to contain all the commands you want to have running at all times, putting one command on each line.

## To use a command list of a different name and/or location:

    ./lars-script-runner -f /path/to/commands.txt
