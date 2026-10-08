/*
 * malina-pamcheck — аутентификация Linux-пользователя через PAM.
 *
 * Пароль читается из stdin (первая строка). Коды выхода:
 *   0  успех
 *   1  аутентификация не прошла
 *   2  ошибка использования
 *   3  libpam недоступна
 *   4  ошибка PAM
 *
 * Используется dlopen(), поэтому на этапе сборки заголовки libpam не нужны.
 *
 * Сборка:  gcc -O2 -o malina-pamcheck pamcheck.c -ldl
 * Запуск:  echo -n "$PASSWORD" | malina-pamcheck [-s service] -u user
 */
#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <dlfcn.h>
#include <unistd.h>

typedef struct pam_handle pam_handle_t;

struct pam_message {
    int msg_style;
    const char *msg;
};

struct pam_response {
    char *resp;
    int resp_retcode;
};

struct pam_conv {
    int (*conv)(int, const struct pam_message **,
                struct pam_response **, void *);
    void *appdata_ptr;
};

#define PAM_SUCCESS 0
#define PAM_PROMPT_ECHO_OFF 1
#define PAM_PROMPT_ECHO_ON 2
#define PAM_BUF_ERR 5

static char *g_password;

static int do_conv(int num_msg, const struct pam_message **msg,
                   struct pam_response **resp, void *data)
{
    (void)data;
    if (num_msg <= 0 || msg == NULL) {
        return PAM_SUCCESS;
    }
    struct pam_response *r = calloc((size_t)num_msg, sizeof(struct pam_response));
    if (r == NULL) {
        return PAM_BUF_ERR;
    }
    for (int i = 0; i < num_msg; i++) {
        if (msg[i] != NULL &&
            (msg[i]->msg_style == PAM_PROMPT_ECHO_OFF ||
             msg[i]->msg_style == PAM_PROMPT_ECHO_ON)) {
            r[i].resp = strdup(g_password != NULL ? g_password : "");
        }
    }
    *resp = r;
    return PAM_SUCCESS;
}

int main(int argc, char **argv)
{
    const char *user = NULL;
    const char *service = "malina-web";
    int opt;

    while ((opt = getopt(argc, argv, "u:s:")) != -1) {
        switch (opt) {
        case 'u': user = optarg; break;
        case 's': service = optarg; break;
        default:
            fprintf(stderr, "usage: %s [-s service] -u user\n", argv[0]);
            return 2;
        }
    }
    if (user == NULL) {
        fprintf(stderr, "usage: %s [-s service] -u user\n", argv[0]);
        return 2;
    }

    char buf[1024];
    if (fgets(buf, sizeof(buf), stdin) == NULL) {
        buf[0] = '\0';
    }
    size_t n = strlen(buf);
    while (n > 0 && (buf[n - 1] == '\n' || buf[n - 1] == '\r')) {
        buf[--n] = '\0';
    }
    g_password = buf;

    void *h = dlopen("libpam.so.0", RTLD_NOW);
    if (h == NULL) {
        fprintf(stderr, "dlopen(libpam.so.0): %s\n", dlerror());
        return 3;
    }

    int (*pam_start)(const char *, const char *,
                     const struct pam_conv *, pam_handle_t **) =
        dlsym(h, "pam_start");
    int (*pam_authenticate)(pam_handle_t *, int) =
        dlsym(h, "pam_authenticate");
    int (*pam_acct_mgmt)(pam_handle_t *, int) =
        dlsym(h, "pam_acct_mgmt");
    int (*pam_end)(pam_handle_t *, int) = dlsym(h, "pam_end");
    if (pam_start == NULL || pam_authenticate == NULL || pam_acct_mgmt == NULL ||
        pam_end == NULL) {
        fprintf(stderr, "dlsym(pam_*): %s\n", dlerror());
        return 3;
    }

    struct pam_conv conv = { do_conv, NULL };
    pam_handle_t *ph = NULL;
    int rc = pam_start(service, user, &conv, &ph);
    if (rc != PAM_SUCCESS) {
        fprintf(stderr, "pam_start rc=%d\n", rc);
        return 4;
    }
    rc = pam_authenticate(ph, 0);
    // Проверяем срок/блокировку учётной записи (acct); при успехе auth.
    if (rc == PAM_SUCCESS) {
        rc = pam_acct_mgmt(ph, 0);
    }
    pam_end(ph, rc);
    if (rc != PAM_SUCCESS) {
        fprintf(stderr, "pam rc=%d\n", rc);
        return 1;
    }
    return 0;
}
