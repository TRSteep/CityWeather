import requests
from datetime import datetime, date, timedelta
# Plot
import matplotlib.pyplot as plt
import matplotlib.dates as mdates
# GEO lib
from geopy.geocoders import Nominatim
from geopy.exc import GeocoderUnavailable, GeocoderTimedOut


def get_coordinates(city):
    """
    Получает координаты города через Nominatim API.
    По умолчанию ищет в России, но поддерживает международные города.
    Возвращает (широта, долгота) или координаты Уфы (54.73780, 55.94188) при ошибке.
    """
    # Если города нет в словаре, используем API Nominatim
    try:
        geolocator = Nominatim(user_agent="weather_app")
        # Сначала пробуем искать с привязкой к России
        location = geolocator.geocode(f"{city}, Россия", exactly_one=True)
        # Если не найдено в России, ищем без указания страны
        if not location:
            location = geolocator.geocode(city, exactly_one=True)

        if location:
            return (location.latitude, location.longitude)
        else:
            print(f"⚠ Город '{city}' не найден. Используются координаты Уфы.")
            return (54.73780, 55.94188)

    except (GeocoderUnavailable, GeocoderTimedOut, requests.exceptions.RequestException) as e:
        print(f"⚠ Ошибка геокодинга: {e}. Используются координаты Уфы.")
        return (54.73780, 55.94188)


def fetch_weather_data(latitude, longitude, start_date, end_date):
    """Запрашивает архивные данные, а если их нет — прогнозные"""
    # Запрос архивных данных
    url = "https://archive-api.open-meteo.com/v1/archive"
    params = {
        "latitude": latitude,
        "longitude": longitude,
        "start_date": start_date,
        "end_date": end_date,
        "daily": "temperature_2m_max,temperature_2m_min",
        "timezone": "auto",
    }
    response = requests.get(url, params=params)
    data = response.json()

    # Если архивных данных нет (например, за вчера/сегодня), запрашиваем прогноз
    if "daily" not in data or any(temp is None for temp in data["daily"]["temperature_2m_max"]):
        forecast_url = "https://api.open-meteo.com/v1/forecast"
        forecast_params = {
            "latitude": latitude,
            "longitude": longitude,
            "daily": "temperature_2m_max,temperature_2m_min",
            "timezone": "auto",
            "forecast_days": 1,  # Только на 1 день вперед
        }
        forecast_response = requests.get(forecast_url, params=forecast_params)
        forecast_data = forecast_response.json()

        # Объединяем архивные и прогнозные данные
        if "daily" in forecast_data:
            for key in ["temperature_2m_max", "temperature_2m_min"]:
                data["daily"][key][-1] = forecast_data["daily"][key][0]  # Заменяем последний день

    return data


def clean_data(temp_data):
    """Заменяет None на предыдущее допустимое значение"""
    clean = []
    last_valid = None
    for temp in temp_data:
        if temp is not None:
            clean.append(temp)
            last_valid = temp
        else:
            clean.append(last_valid if last_valid is not None else 0)  # Если нет данных, ставим 0
    return clean


def plot_weather(data, city):
    """Строит график температуры с аннотациями и легендой"""
    dates = [datetime.strptime(d, "%Y-%m-%d").date() for d in data["daily"]["time"]]
    temp_max = data["daily"]["temperature_2m_max"]
    temp_min = data["daily"]["temperature_2m_min"]

    # Убираем дни, где данные отсутствуют (None)
    valid_dates = []
    valid_max = []
    valid_min = []
    for i in range(len(dates)):
        if temp_max[i] is not None and temp_min[i] is not None:
            valid_dates.append(dates[i])
            valid_max.append(temp_max[i])
            valid_min.append(temp_min[i])

    if not valid_dates:
        print("⚠ Нет данных для построения графика!")
        return

    # Дальше рисуем график как обычно, но с valid_dates, valid_max, valid_min
    plt.figure(figsize=(12, 6))

    # Графики с маркерами
    max_line, = plt.plot(dates, temp_max, label="Макс. температура", marker="o", color="red", linestyle="-",
                         linewidth=2)
    min_line, = plt.plot(dates, temp_min, label="Мин. температура", marker="o", color="blue", linestyle="-",
                         linewidth=2)

    # Аннотации для максимальной температуры
    for date_, temp in zip(dates, temp_max):
        plt.annotate(
            f"{temp:.1f}°C",
            (date_, temp),
            textcoords="offset points",
            xytext=(0, 10),
            ha="center",
            fontsize=9,
            color="red",
        )

    # Аннотации для минимальной температуры
    for date_, temp in zip(dates, temp_min):
        plt.annotate(
            f"{temp:.1f}°C",
            (date_, temp),
            textcoords="offset points",
            xytext=(0, -15),
            ha="center",
            fontsize=9,
            color="blue",
        )

    # Заливка между графиками
    plt.fill_between(dates, temp_min, temp_max, color="lightgray", alpha=0.3, label="Разница температур")

    # Настройка легенды
    plt.legend(
        handles=[max_line, min_line],
        loc="upper left",
        framealpha=1,
        shadow=True,
        fontsize=10,
    )

    # Настройка осей и заголовка
    plt.title(f"Температура в {city.capitalize()} за последние 7 дней", fontsize=14, pad=20)
    plt.xlabel("Дата", fontsize=12)
    plt.ylabel("Температура (°C)", fontsize=12)
    plt.grid(True, linestyle="--", alpha=0.7)

    # Форматирование дат
    plt.gca().xaxis.set_major_formatter(mdates.DateFormatter("%d.%m"))
    plt.gca().xaxis.set_major_locator(mdates.DayLocator())
    plt.gcf().autofmt_xdate()

    plt.tight_layout()
    plt.show()


def main():
    print("🌦️ Программа для анализа погоды (Open-Meteo)")
    print("-------------------------------------------")

    city = input("Введите город (Enter для Уфы): ").strip()
    if city == "":
        city = "Уфа"
    date_str = input("Введите дату в формате ГГГГ-ММ-ДД (Enter для последних 7 дней): ").strip()

    latitude, longitude = get_coordinates(city)

    if date_str:
        # Погода на конкретную дату
        try:
            target_date = datetime.strptime(date_str, "%Y-%m-%d").date()
            data = fetch_weather_data(latitude, longitude, date_str, date_str)
            print(f"Координаты города {city}: {latitude}, {longitude}")

            if "daily" in data:
                temp_max = data["daily"]["temperature_2m_max"][0]
                temp_min = data["daily"]["temperature_2m_min"][0]
                print(f"\n📅 Погода в {city.capitalize()} на {date_str}:")
                print(f"🔥 Максимальная температура: {temp_max}°C")
                print(f"❄️ Минимальная температура: {temp_min}°C")
            else:
                print("Ошибка: Нет данных на эту дату.")
        except ValueError:
            print("⚠ Неверный формат даты!")
    else:
        # Погода за последние 7 дней
        end_date = date.today() - timedelta(days=1)
        start_date = end_date - timedelta(days=7)
        data = fetch_weather_data(latitude, longitude, start_date.strftime("%Y-%m-%d"), end_date.strftime("%Y-%m-%d"))

        if "daily" in data:
            plot_weather(data, city)
        else:
            print("Ошибка: Не удалось получить данные.")


if __name__ == "__main__":
    main()