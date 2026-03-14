#include <jni.h>
#include <dlfcn.h>
#include <android/log.h>
#include <pthread.h>
#include <stdio.h>

typedef char *(*bridge_noarg_fn)(void);
typedef char *(*bridge_arg_fn)(const char *);
typedef char *(*bridge_arg_int_fn)(const char *, int);
typedef char *(*bridge_arg2_fn)(const char *, const char *);
typedef char *(*bridge_arg3_fn)(const char *, const char *, const char *);
typedef void (*bridge_free_fn)(char *);
typedef void (*bridge_set_route_fn)(const char *);
typedef void (*bridge_set_snapshot_fn)(const char *);
typedef int (*bridge_query_uid_fn)(int, const char *, int, const char *, int, char *, size_t);

static void *bridge_handle = NULL;
static bridge_noarg_fn bridge_version = NULL;
static bridge_arg_fn bridge_status = NULL;
static bridge_arg_fn bridge_login = NULL;
static bridge_arg_fn bridge_logout = NULL;
static bridge_arg_int_fn bridge_start = NULL;
static bridge_arg_fn bridge_stop = NULL;
static bridge_arg2_fn bridge_set_proxy_mode = NULL;
static bridge_arg3_fn bridge_select_proxy_group = NULL;
static bridge_free_fn bridge_free = NULL;
static bridge_set_route_fn bridge_set_default_route = NULL;
static bridge_set_snapshot_fn bridge_set_interface_snapshot = NULL;
static bridge_set_snapshot_fn bridge_set_installed_apps_snapshot = NULL;
static pthread_mutex_t bridge_mutex = PTHREAD_MUTEX_INITIALIZER;
static JavaVM *bridge_vm = NULL;
static jobject vpn_service = NULL;
static jmethodID vpn_protect_method = NULL;
static jmethodID vpn_query_uid_method = NULL;
static pthread_mutex_t vpn_service_mutex = PTHREAD_MUTEX_INITIALIZER;

static jstring invoke_string_callback(JNIEnv *env, bridge_arg_fn callback, jstring input);

static void write_error(char *errbuf, size_t errlen, const char *message) {
    if (errbuf == NULL || errlen == 0) {
        return;
    }
    snprintf(errbuf, errlen, "%s", message != NULL ? message : "unknown error");
}

static void clear_vpn_service_locked(JNIEnv *env) {
    if (vpn_service != NULL) {
        (*env)->DeleteGlobalRef(env, vpn_service);
        vpn_service = NULL;
    }
    vpn_protect_method = NULL;
    vpn_query_uid_method = NULL;
}

static int set_vpn_service_locked(JNIEnv *env, jobject service) {
    clear_vpn_service_locked(env);
    if (service == NULL) {
        return 1;
    }

    jobject global_service = (*env)->NewGlobalRef(env, service);
    if (global_service == NULL) {
        __android_log_print(ANDROID_LOG_ERROR, "homoscale_jni", "NewGlobalRef for VpnService failed");
        return 0;
    }

    jclass service_class = (*env)->GetObjectClass(env, service);
    if (service_class == NULL) {
        __android_log_print(ANDROID_LOG_ERROR, "homoscale_jni", "GetObjectClass for VpnService failed");
        (*env)->DeleteGlobalRef(env, global_service);
        return 0;
    }

    jmethodID protect_method = (*env)->GetMethodID(env, service_class, "protect", "(I)Z");
    jmethodID query_uid_method = (*env)->GetMethodID(
            env,
            service_class,
            "queryConnectionOwnerUid",
            "(ILjava/lang/String;ILjava/lang/String;I)I"
    );
    (*env)->DeleteLocalRef(env, service_class);
    if (protect_method == NULL || query_uid_method == NULL) {
        __android_log_print(
                ANDROID_LOG_ERROR,
                "homoscale_jni",
                "GetMethodID(VpnService callbacks) failed protect=%p queryUid=%p",
                protect_method,
                query_uid_method
        );
        (*env)->DeleteGlobalRef(env, global_service);
        return 0;
    }

    vpn_service = global_service;
    vpn_protect_method = protect_method;
    vpn_query_uid_method = query_uid_method;
    return 1;
}

jint JNI_OnLoad(JavaVM *vm, void *reserved) {
    (void) reserved;
    bridge_vm = vm;
    return JNI_VERSION_1_6;
}

static int ensure_bridge_loaded(void) {
    if (bridge_handle != NULL &&
        bridge_version != NULL &&
        bridge_status != NULL &&
        bridge_login != NULL &&
        bridge_logout != NULL &&
        bridge_start != NULL &&
        bridge_stop != NULL &&
        bridge_set_proxy_mode != NULL &&
        bridge_select_proxy_group != NULL &&
        bridge_free != NULL &&
        bridge_set_default_route != NULL &&
        bridge_set_interface_snapshot != NULL &&
        bridge_set_installed_apps_snapshot != NULL) {
        return 1;
    }

    pthread_mutex_lock(&bridge_mutex);
    if (bridge_handle != NULL &&
        bridge_version != NULL &&
        bridge_status != NULL &&
        bridge_login != NULL &&
        bridge_logout != NULL &&
        bridge_start != NULL &&
        bridge_stop != NULL &&
        bridge_set_proxy_mode != NULL &&
        bridge_select_proxy_group != NULL &&
        bridge_free != NULL &&
        bridge_set_default_route != NULL &&
        bridge_set_interface_snapshot != NULL &&
        bridge_set_installed_apps_snapshot != NULL) {
        pthread_mutex_unlock(&bridge_mutex);
        return 1;
    }

    void *handle = dlopen("libhomoscale.so", RTLD_NOW | RTLD_GLOBAL);
    if (handle == NULL) {
        __android_log_print(ANDROID_LOG_ERROR, "homoscale_jni", "dlopen failed: %s", dlerror());
        pthread_mutex_unlock(&bridge_mutex);
        return 0;
    }

    bridge_noarg_fn version = (bridge_noarg_fn) dlsym(handle, "HomoscaleVersionJSON");
    bridge_arg_fn status = (bridge_arg_fn) dlsym(handle, "HomoscaleStatusJSON");
    bridge_arg_fn login = (bridge_arg_fn) dlsym(handle, "HomoscaleLoginJSON");
    bridge_arg_fn logout = (bridge_arg_fn) dlsym(handle, "HomoscaleLogoutJSON");
    bridge_arg_int_fn start = (bridge_arg_int_fn) dlsym(handle, "HomoscaleStartWithTunFDJSON");
    bridge_arg_fn stop = (bridge_arg_fn) dlsym(handle, "HomoscaleStopJSON");
    bridge_arg2_fn set_proxy_mode = (bridge_arg2_fn) dlsym(handle, "HomoscaleSetProxyModeJSON");
    bridge_arg3_fn select_proxy_group = (bridge_arg3_fn) dlsym(handle, "HomoscaleSelectProxyGroupJSON");
    bridge_free_fn free_fn = (bridge_free_fn) dlsym(handle, "HomoscaleFreeString");
    bridge_set_route_fn set_default_route = (bridge_set_route_fn) dlsym(handle, "HomoscaleSetAndroidDefaultRouteInterface");
    bridge_set_snapshot_fn set_interface_snapshot = (bridge_set_snapshot_fn) dlsym(handle, "HomoscaleSetAndroidInterfaceSnapshot");
    bridge_set_snapshot_fn set_installed_apps_snapshot = (bridge_set_snapshot_fn) dlsym(handle, "HomoscaleSetAndroidInstalledAppsSnapshot");

    if (version == NULL || status == NULL || login == NULL || logout == NULL ||
        start == NULL || stop == NULL || set_proxy_mode == NULL || select_proxy_group == NULL || free_fn == NULL ||
        set_default_route == NULL || set_interface_snapshot == NULL || set_installed_apps_snapshot == NULL) {
        __android_log_print(
                ANDROID_LOG_ERROR,
                "homoscale_jni",
                "dlsym failed: version=%p status=%p login=%p logout=%p start=%p stop=%p setMode=%p select=%p free=%p setRoute=%p setSnapshot=%p setApps=%p",
                version, status, login, logout, start, stop, set_proxy_mode, select_proxy_group, free_fn, set_default_route, set_interface_snapshot, set_installed_apps_snapshot
        );
        dlclose(handle);
        pthread_mutex_unlock(&bridge_mutex);
        return 0;
    }

    bridge_handle = handle;
    bridge_version = version;
    bridge_status = status;
    bridge_login = login;
    bridge_logout = logout;
    bridge_start = start;
    bridge_stop = stop;
    bridge_set_proxy_mode = set_proxy_mode;
    bridge_select_proxy_group = select_proxy_group;
    bridge_free = free_fn;
    bridge_set_default_route = set_default_route;
    bridge_set_interface_snapshot = set_interface_snapshot;
    bridge_set_installed_apps_snapshot = set_installed_apps_snapshot;
    pthread_mutex_unlock(&bridge_mutex);
    return 1;
}

static jstring invoke_noarg(JNIEnv *env) {
    if (!ensure_bridge_loaded()) {
        return (*env)->NewStringUTF(env, "{\"ok\":false,\"error\":\"bridge load failed\"}");
    }
    char *result = bridge_version();
    jstring output = (*env)->NewStringUTF(env, result != NULL ? result : "{\"ok\":false,\"error\":\"empty bridge response\"}");
    if (result != NULL) {
        bridge_free(result);
    }
    return output;
}

static jstring invoke_status(JNIEnv *env, jstring input) {
    if (!ensure_bridge_loaded()) {
        return (*env)->NewStringUTF(env, "{\"ok\":false,\"error\":\"bridge load failed\"}");
    }
    return invoke_string_callback(env, bridge_status, input);
}

static jstring invoke_string_callback(JNIEnv *env, bridge_arg_fn callback, jstring input) {
    if (callback == NULL) {
        return (*env)->NewStringUTF(env, "{\"ok\":false,\"error\":\"bridge callback unavailable\"}");
    }

    const char *text = "";
    if (input != NULL) {
        text = (*env)->GetStringUTFChars(env, input, NULL);
    }

    char *result = callback(text);

    if (input != NULL) {
        (*env)->ReleaseStringUTFChars(env, input, text);
    }

    jstring output = (*env)->NewStringUTF(env, result != NULL ? result : "{\"ok\":false,\"error\":\"empty bridge response\"}");
    if (result != NULL) {
        bridge_free(result);
    }
    return output;
}

static jstring invoke_stop(JNIEnv *env, jstring input) {
    if (!ensure_bridge_loaded()) {
        return (*env)->NewStringUTF(env, "{\"ok\":false,\"error\":\"bridge load failed\"}");
    }
    return invoke_string_callback(env, bridge_stop, input);
}

static jstring invoke_with_string_and_int(JNIEnv *env, jstring input, jint value) {
    if (!ensure_bridge_loaded()) {
        return (*env)->NewStringUTF(env, "{\"ok\":false,\"error\":\"bridge load failed\"}");
    }

    const char *text = "";
    if (input != NULL) {
        text = (*env)->GetStringUTFChars(env, input, NULL);
    }

    char *result = bridge_start(text, value);

    if (input != NULL) {
        (*env)->ReleaseStringUTFChars(env, input, text);
    }

    jstring output = (*env)->NewStringUTF(env, result != NULL ? result : "{\"ok\":false,\"error\":\"empty bridge response\"}");
    if (result != NULL) {
        bridge_free(result);
    }
    return output;
}

static jstring invoke_with_two_strings(JNIEnv *env, bridge_arg2_fn callback, jstring first, jstring second) {
    if (callback == NULL) {
        return (*env)->NewStringUTF(env, "{\"ok\":false,\"error\":\"bridge callback unavailable\"}");
    }

    const char *left = "";
    const char *right = "";
    if (first != NULL) {
        left = (*env)->GetStringUTFChars(env, first, NULL);
    }
    if (second != NULL) {
        right = (*env)->GetStringUTFChars(env, second, NULL);
    }

    char *result = callback(left, right);

    if (second != NULL) {
        (*env)->ReleaseStringUTFChars(env, second, right);
    }
    if (first != NULL) {
        (*env)->ReleaseStringUTFChars(env, first, left);
    }

    jstring output = (*env)->NewStringUTF(env, result != NULL ? result : "{\"ok\":false,\"error\":\"empty bridge response\"}");
    if (result != NULL) {
        bridge_free(result);
    }
    return output;
}

static jstring invoke_with_three_strings(JNIEnv *env, bridge_arg3_fn callback, jstring first, jstring second, jstring third) {
    if (callback == NULL) {
        return (*env)->NewStringUTF(env, "{\"ok\":false,\"error\":\"bridge callback unavailable\"}");
    }

    const char *one = "";
    const char *two = "";
    const char *three = "";
    if (first != NULL) {
        one = (*env)->GetStringUTFChars(env, first, NULL);
    }
    if (second != NULL) {
        two = (*env)->GetStringUTFChars(env, second, NULL);
    }
    if (third != NULL) {
        three = (*env)->GetStringUTFChars(env, third, NULL);
    }

    char *result = callback(one, two, three);

    if (third != NULL) {
        (*env)->ReleaseStringUTFChars(env, third, three);
    }
    if (second != NULL) {
        (*env)->ReleaseStringUTFChars(env, second, two);
    }
    if (first != NULL) {
        (*env)->ReleaseStringUTFChars(env, first, one);
    }

    jstring output = (*env)->NewStringUTF(env, result != NULL ? result : "{\"ok\":false,\"error\":\"empty bridge response\"}");
    if (result != NULL) {
        bridge_free(result);
    }
    return output;
}

JNIEXPORT jstring JNICALL
Java_io_homoscale_android_HomoscaleBridge_version(JNIEnv *env, jclass clazz) {
    (void) clazz;
    return invoke_noarg(env);
}

JNIEXPORT jstring JNICALL
Java_io_homoscale_android_HomoscaleBridge_status(JNIEnv *env, jclass clazz, jstring config_path) {
    (void) clazz;
    return invoke_status(env, config_path);
}

JNIEXPORT jstring JNICALL
Java_io_homoscale_android_HomoscaleBridge_login(JNIEnv *env, jclass clazz, jstring config_path) {
    (void) clazz;
    if (!ensure_bridge_loaded()) {
        return (*env)->NewStringUTF(env, "{\"ok\":false,\"error\":\"bridge load failed\"}");
    }
    return invoke_string_callback(env, bridge_login, config_path);
}

JNIEXPORT jstring JNICALL
Java_io_homoscale_android_HomoscaleBridge_logout(JNIEnv *env, jclass clazz, jstring config_path) {
    (void) clazz;
    if (!ensure_bridge_loaded()) {
        return (*env)->NewStringUTF(env, "{\"ok\":false,\"error\":\"bridge load failed\"}");
    }
    return invoke_string_callback(env, bridge_logout, config_path);
}

JNIEXPORT jstring JNICALL
Java_io_homoscale_android_HomoscaleBridge_start(JNIEnv *env, jclass clazz, jstring config_path, jint tun_fd) {
    (void) clazz;
    return invoke_with_string_and_int(env, config_path, tun_fd);
}

JNIEXPORT jstring JNICALL
Java_io_homoscale_android_HomoscaleBridge_stop(JNIEnv *env, jclass clazz, jstring config_path) {
    (void) clazz;
    return invoke_stop(env, config_path);
}

JNIEXPORT jstring JNICALL
Java_io_homoscale_android_HomoscaleBridge_setProxyMode(JNIEnv *env, jclass clazz, jstring config_path, jstring mode) {
    (void) clazz;
    if (!ensure_bridge_loaded()) {
        return (*env)->NewStringUTF(env, "{\"ok\":false,\"error\":\"bridge load failed\"}");
    }
    return invoke_with_two_strings(env, bridge_set_proxy_mode, config_path, mode);
}

JNIEXPORT jstring JNICALL
Java_io_homoscale_android_HomoscaleBridge_selectProxyGroup(JNIEnv *env, jclass clazz, jstring config_path, jstring group_name, jstring proxy_name) {
    (void) clazz;
    if (!ensure_bridge_loaded()) {
        return (*env)->NewStringUTF(env, "{\"ok\":false,\"error\":\"bridge load failed\"}");
    }
    return invoke_with_three_strings(env, bridge_select_proxy_group, config_path, group_name, proxy_name);
}

JNIEXPORT void JNICALL
Java_io_homoscale_android_HomoscaleBridge_installVpnService(JNIEnv *env, jclass clazz, jobject service) {
    (void) clazz;
    pthread_mutex_lock(&vpn_service_mutex);
    if (!set_vpn_service_locked(env, service)) {
        clear_vpn_service_locked(env);
        __android_log_print(ANDROID_LOG_ERROR, "homoscale_jni", "installVpnService failed");
    } else {
        __android_log_print(ANDROID_LOG_INFO, "homoscale_jni", "installVpnService registered");
    }
    pthread_mutex_unlock(&vpn_service_mutex);
}

JNIEXPORT void JNICALL
Java_io_homoscale_android_HomoscaleBridge_clearVpnService(JNIEnv *env, jclass clazz) {
    (void) clazz;
    pthread_mutex_lock(&vpn_service_mutex);
    clear_vpn_service_locked(env);
    pthread_mutex_unlock(&vpn_service_mutex);
    __android_log_print(ANDROID_LOG_INFO, "homoscale_jni", "clearVpnService completed");
}

JNIEXPORT void JNICALL
Java_io_homoscale_android_HomoscaleBridge_setDefaultRouteInterface(JNIEnv *env, jclass clazz, jstring interface_name) {
    (void) clazz;
    if (!ensure_bridge_loaded()) {
        __android_log_print(ANDROID_LOG_ERROR, "homoscale_jni", "setDefaultRouteInterface failed: bridge load failed");
        return;
    }
    const char *text = "";
    if (interface_name != NULL) {
        text = (*env)->GetStringUTFChars(env, interface_name, NULL);
    }
    bridge_set_default_route(text);
    __android_log_print(ANDROID_LOG_INFO, "homoscale_jni", "default route interface=%s", text);
    if (interface_name != NULL) {
        (*env)->ReleaseStringUTFChars(env, interface_name, text);
    }
}

JNIEXPORT void JNICALL
Java_io_homoscale_android_HomoscaleBridge_setInterfaceSnapshot(JNIEnv *env, jclass clazz, jstring snapshot_json) {
    (void) clazz;
    if (!ensure_bridge_loaded()) {
        __android_log_print(ANDROID_LOG_ERROR, "homoscale_jni", "setInterfaceSnapshot failed: bridge load failed");
        return;
    }
    const char *text = "[]";
    if (snapshot_json != NULL) {
        text = (*env)->GetStringUTFChars(env, snapshot_json, NULL);
    }
    bridge_set_interface_snapshot(text);
    __android_log_print(ANDROID_LOG_INFO, "homoscale_jni", "interface snapshot updated");
    if (snapshot_json != NULL) {
        (*env)->ReleaseStringUTFChars(env, snapshot_json, text);
    }
}

JNIEXPORT void JNICALL
Java_io_homoscale_android_HomoscaleBridge_setInstalledAppsSnapshot(JNIEnv *env, jclass clazz, jstring snapshot_json) {
    (void) clazz;
    if (!ensure_bridge_loaded()) {
        __android_log_print(ANDROID_LOG_ERROR, "homoscale_jni", "setInstalledAppsSnapshot failed: bridge load failed");
        return;
    }
    const char *text = "[]";
    if (snapshot_json != NULL) {
        text = (*env)->GetStringUTFChars(env, snapshot_json, NULL);
    }
    bridge_set_installed_apps_snapshot(text);
    __android_log_print(ANDROID_LOG_INFO, "homoscale_jni", "installed apps snapshot updated");
    if (snapshot_json != NULL) {
        (*env)->ReleaseStringUTFChars(env, snapshot_json, text);
    }
}

JNIEXPORT int JNICALL
HomoscaleProtectSocketFD(int fd, char *errbuf, size_t errlen) {
    if (bridge_vm == NULL) {
        write_error(errbuf, errlen, "JavaVM unavailable");
        return -1;
    }

    JNIEnv *env = NULL;
    jint env_status = (*bridge_vm)->GetEnv(bridge_vm, (void **) &env, JNI_VERSION_1_6);
    int should_detach = 0;
    if (env_status == JNI_EDETACHED) {
        if ((*bridge_vm)->AttachCurrentThread(bridge_vm, &env, NULL) != JNI_OK) {
            write_error(errbuf, errlen, "AttachCurrentThread failed");
            return -1;
        }
        should_detach = 1;
    } else if (env_status != JNI_OK) {
        write_error(errbuf, errlen, "GetEnv failed");
        return -1;
    }

    jobject service = NULL;
    jmethodID protect_method = NULL;

    pthread_mutex_lock(&vpn_service_mutex);
    if (vpn_service != NULL) {
        service = (*env)->NewLocalRef(env, vpn_service);
        protect_method = vpn_protect_method;
    }
    pthread_mutex_unlock(&vpn_service_mutex);

    if (service == NULL || protect_method == NULL) {
        if (service != NULL) {
            (*env)->DeleteLocalRef(env, service);
        }
        if (should_detach) {
            (*bridge_vm)->DetachCurrentThread(bridge_vm);
        }
        __android_log_print(ANDROID_LOG_INFO, "homoscale_jni", "protect fd=%d skipped: VpnService callback unavailable", fd);
        return 0;
    }

    jboolean ok = (*env)->CallBooleanMethod(env, service, protect_method, (jint) fd);
    if ((*env)->ExceptionCheck(env)) {
        __android_log_print(ANDROID_LOG_ERROR, "homoscale_jni", "VpnService.protect threw");
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
        (*env)->DeleteLocalRef(env, service);
        if (should_detach) {
            (*bridge_vm)->DetachCurrentThread(bridge_vm);
        }
        write_error(errbuf, errlen, "VpnService.protect threw");
        __android_log_print(ANDROID_LOG_ERROR, "homoscale_jni", "protect fd=%d failed: %s", fd, errbuf);
        return -1;
    }

    (*env)->DeleteLocalRef(env, service);
    if (should_detach) {
        (*bridge_vm)->DetachCurrentThread(bridge_vm);
    }
    if (!ok) {
        write_error(errbuf, errlen, "VpnService.protect returned false");
        __android_log_print(ANDROID_LOG_ERROR, "homoscale_jni", "protect fd=%d failed: %s", fd, errbuf);
        return -1;
    }
    __android_log_print(ANDROID_LOG_INFO, "homoscale_jni", "protect fd=%d ok", fd);
    return 0;
}

JNIEXPORT int JNICALL
HomoscaleQueryConnectionOwnerUID(int protocol, const char *source_addr, int source_port, const char *target_addr, int target_port, char *errbuf, size_t errlen) {
    if (bridge_vm == NULL) {
        write_error(errbuf, errlen, "JavaVM unavailable");
        return -1;
    }

    JNIEnv *env = NULL;
    jint env_status = (*bridge_vm)->GetEnv(bridge_vm, (void **) &env, JNI_VERSION_1_6);
    int should_detach = 0;
    if (env_status == JNI_EDETACHED) {
        if ((*bridge_vm)->AttachCurrentThread(bridge_vm, &env, NULL) != JNI_OK) {
            write_error(errbuf, errlen, "AttachCurrentThread failed");
            return -1;
        }
        should_detach = 1;
    } else if (env_status != JNI_OK) {
        write_error(errbuf, errlen, "GetEnv failed");
        return -1;
    }

    jobject service = NULL;
    jmethodID query_uid_method = NULL;

    pthread_mutex_lock(&vpn_service_mutex);
    if (vpn_service != NULL) {
        service = (*env)->NewLocalRef(env, vpn_service);
        query_uid_method = vpn_query_uid_method;
    }
    pthread_mutex_unlock(&vpn_service_mutex);

    if (service == NULL || query_uid_method == NULL) {
        if (service != NULL) {
            (*env)->DeleteLocalRef(env, service);
        }
        if (should_detach) {
            (*bridge_vm)->DetachCurrentThread(bridge_vm);
        }
        write_error(errbuf, errlen, "VpnService query uid callback unavailable");
        return -1;
    }

    jstring source = (*env)->NewStringUTF(env, source_addr != NULL ? source_addr : "");
    jstring target = (*env)->NewStringUTF(env, target_addr != NULL ? target_addr : "");
    if (source == NULL || target == NULL) {
        if (source != NULL) {
            (*env)->DeleteLocalRef(env, source);
        }
        if (target != NULL) {
            (*env)->DeleteLocalRef(env, target);
        }
        (*env)->DeleteLocalRef(env, service);
        if (should_detach) {
            (*bridge_vm)->DetachCurrentThread(bridge_vm);
        }
        write_error(errbuf, errlen, "NewStringUTF failed");
        return -1;
    }

    jint uid = (*env)->CallIntMethod(env, service, query_uid_method, (jint) protocol, source, (jint) source_port, target, (jint) target_port);
    if ((*env)->ExceptionCheck(env)) {
        __android_log_print(ANDROID_LOG_ERROR, "homoscale_jni", "queryConnectionOwnerUid threw");
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
        uid = -1;
        write_error(errbuf, errlen, "queryConnectionOwnerUid threw");
    }

    (*env)->DeleteLocalRef(env, source);
    (*env)->DeleteLocalRef(env, target);
    (*env)->DeleteLocalRef(env, service);
    if (should_detach) {
        (*bridge_vm)->DetachCurrentThread(bridge_vm);
    }
    if (uid < 0 && errbuf != NULL && errlen > 0 && errbuf[0] == '\0') {
        write_error(errbuf, errlen, "queryConnectionOwnerUid returned no result");
    }
    return (int) uid;
}
