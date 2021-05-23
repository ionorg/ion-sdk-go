#ifndef GST_H
#define GST_H

#include <glib.h>
#include <gst/gst.h>
#include <stdint.h>
#include <stdbool.h>
#include <stdlib.h>

extern void goHandlePipelineBuffer(void *buffer, int bufferLen, int samples, char *localTrackID);
extern void goHandleAppsrcForceKeyUnit(char *remote_track_id);

void gstreamer_start_mainloop(void);

// Gstreamer Pipeline controls
GstElement *gstreamer_create_pipeline(char *pipeline);
void gstreamer_start_pipeline(GstElement *pipeline);
void gstreamer_stop_pipeline(GstElement *pipeline); 
void gstreamer_play_pipeline(GstElement *pipeline);
void gstreamer_pause_pipeline(GstElement *pipeline);
void gstreamer_seek(GstElement *pipeline, int64_t seek_pos);


// Helpers for push/pull of buffers from go
void gstreamer_send_bind_appsink_track(GstElement *pipeline, char *appSinkName, char *trackId);
void gstreamer_receive_push_buffer(GstElement *pipeline, void *buffer, int len, char* element_name);

// Helpers for the compositor_pipeline
GstElement *gstreamer_compositor_add_input_track(GstElement *pipeline, char *input_description, char *track_id, bool isVideo);
void gstreamer_compositor_remove_input_track(GstElement *pipeline, GstElement *input_bin, bool isVideo); 
void gstreamer_compositor_relayout_videos(GstElement *compositor); 
#endif
