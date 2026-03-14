cmake_minimum_required(VERSION 3.22.1)

project(homoscale_jni C)

add_library(homoscale_jni SHARED homoscale_jni.c)

find_library(log-lib log)

target_link_libraries(homoscale_jni ${log-lib} dl)
