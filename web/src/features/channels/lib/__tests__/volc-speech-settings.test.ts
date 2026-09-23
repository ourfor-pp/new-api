/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, test } from 'vitest'

import { channelSchema } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
  transformChannelToFormDefaults,
} from '../channel-form'

describe('VolcEngine speech channel settings', () => {
  test('preserves speaker and platform hotword settings when reopening and saving a channel', () => {
    const form = {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 45,
      name: 'speech',
      models: 'sxh-tts,sxh-asr',
      volc_speech_default_tts_speaker: ' speaker-id ',
      volc_speech_asr_hotword_table_id: ' hotword-id ',
    }
    const created = transformFormDataToCreatePayload(form).channel
    expect(JSON.parse(created.settings ?? '{}')).toMatchObject({
      volc_speech: {
        default_tts_speaker: 'speaker-id',
        asr_hotword_table_id: 'hotword-id',
      },
    })
    const channel = channelSchema.parse({
      ...created,
      id: 1,
      created_time: 1,
      key: 'fixture-key',
      test_time: 0,
      response_time: 0,
      other: '',
      balance_updated_time: 0,
      remark: '',
    })
    const reopened = transformChannelToFormDefaults(channel)
    expect(reopened.volc_speech_default_tts_speaker).toBe('speaker-id')
    expect(reopened.volc_speech_asr_hotword_table_id).toBe('hotword-id')
    const updated = transformFormDataToUpdatePayload(
      { ...CHANNEL_FORM_DEFAULT_VALUES, ...reopened },
      1
    )
    expect(JSON.parse(updated.settings ?? '{}').volc_speech).toEqual(
      JSON.parse(created.settings ?? '{}').volc_speech
    )
  })
  test('removes speech settings when changing to a non-VolcEngine channel', () => {
    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 1,
      settings: JSON.stringify({
        volc_speech: { default_tts_speaker: 'speaker-id' },
      }),
      volc_speech_default_tts_speaker: 'speaker-id',
    })
    expect(JSON.parse(payload.channel.settings ?? '{}')).not.toHaveProperty(
      'volc_speech'
    )
  })
})
