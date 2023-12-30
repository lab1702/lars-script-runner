# lars-script-runner

Tiny command line tool that will make sure a list of scripts or other commands are always running.

## How to install:

    git clone https://github.com/lab1702/lars-script-runner.git
    cd lars-script-runner
    go build

## How to configure:

Edit the **commands.txt** file to contain all the commands you want to have running at all times, putting one command on each line.

## How to run:

    ./lars-script-runner.exe

If not on Windows you'll want to drop the **.exe** part.
