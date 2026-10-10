#!/bin/bash -e
# Обновление до текущего состояния публичных репозиториев (пакеты проекта уже
# установлены выше по списку 00-packages, upgrade подтягивает их свежие версии).
export DEBIAN_FRONTEND=noninteractive
apt-get -y update
apt-get -y upgrade
apt-get -y autoremove
